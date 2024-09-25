/*
 Copyright 2021 Qiniu Cloud (qiniu.com)
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
     http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiniu/goc/v2/pkg/log"
	"golang.org/x/tools/cover"
	"k8s.io/test-infra/gopherage/pkg/cov"
)

// listAgents return all service informations
func (gs *gocServer) listAgents(c *gin.Context) {
	idQuery := c.Query("id")
	ifInIdMap := idMaps(idQuery)

	agents := make([]*gocCoveredAgent, 0)

	gs.agents.Range(func(key, value interface{}) bool {
		// check if id is in the query ids
		if !ifInIdMap(key.(string)) {
			return true
		}
		fmt.Printf("||||||   gs.agents;%v,%v\n", key, value)

		agent, ok := value.(*gocCoveredAgent)
		fmt.Printf("||||||  agent:%v\n\n", value)
		if !ok {
			return false
		}
		agents = append(agents, agent)
		return true
	})

	c.JSON(http.StatusOK, gin.H{
		"items": agents,
	})
}

func (gs *gocServer) removeAgent(id string) {
	// 当rpc  disconnected且没有缓存文件时，清掉agent

	err := gs.removeAgentFromStore(id)
	if err != nil {
		log.Errorf("fail to remove agent: %v", id)
		err = fmt.Errorf("fail to remove agent: %v, err: %v", id, err)
	}
	gs.agents.Delete(id)
}

func (gs *gocServer) removeAgents(c *gin.Context) {
	idQuery := c.Query("id")
	ifInIdMap := idMaps(idQuery)

	errs := ""
	gs.agents.Range(func(key, value interface{}) bool {

		// check if id is in the query ids
		id := key.(string)
		if !ifInIdMap(id) {
			return true
		}

		agent, ok := value.(*gocCoveredAgent)
		if !ok {
			return false
		}

		err := gs.removeAgentFromStore(id)
		if err != nil {
			log.Errorf("fail to remove agent: %v", id)
			err := fmt.Errorf("fail to remove agent: %v, err: %v", id, err)
			errs = errs + err.Error()
			return true
		}
		agent.closeConnection()
		gs.agents.Delete(key)

		return true
	})

	if errs != "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": errs,
		})
	} else {
		c.JSON(http.StatusOK, nil)
	}
}

func (gs *gocServer) getProfiles_html(c *gin.Context) {
	baseBranch := c.Query("base")
	if baseBranch == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "缺少base参数，值为分支名字",
		})
		return
	}
	GoProjDir := os.Getenv("GO_PROJ_DIR")
	if GoProjDir == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "缺少环境变量 GO_PROJ_DIR，值为maigo工程路径",
		})
		return
	}
	diffType := c.Query("type")
	extra := c.Query("extra")

	merged, err := gs.getMergedProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": err.Error(),
		})
		return
	}

	var buff bytes.Buffer
	err = cov.DumpProfile(merged, &buff)
	log.Infof("cov.dumpprofile err", err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": err.Error(),
		})
		return
	}
	// ------------    generate html    -------------
	// 改变当前工作目录
	err = os.Chdir(GoProjDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "change to project dir failed!" + err.Error(),
		})
		return
	}
	// 定义输入和输出文件名
	inputFile := "coverage" + extra + ".cov"
	outputHTMLFilePath := "coverage" + extra + ".html" // 生成的HTML/XML报告文件路径
	reportHTMLFilePath := "report" + extra + ".html"
	// 判断是否处于流离标头状态，切换代码的方法不一样
	detached, err := isDetachedHead()
	if err != nil {
		fmt.Println("Error checking Git status:", err)
		return
	}
	branchExist, err := isBranchExist(baseBranch)
	fmt.Println("|||||  branch exist", branchExist)
	if err == nil && !branchExist {
		fmt.Println("|||||| branch not exist, get from cache file")
		var htmlContent []byte
		var fileErr error
		if diffType != "diff" {
			htmlContent, fileErr = ioutil.ReadFile(outputHTMLFilePath)

		} else {
			htmlContent, fileErr = ioutil.ReadFile(reportHTMLFilePath)

		}
		if fileErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"msg": "read file reporthtml file path failed" + fileErr.Error(),
			})
			return
		}
		// 将HTML内容发送给HTTP客户端
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, string(htmlContent))
		return

	}
	log.Infof(" branch exist, generate new diff result")
	cmdChangeBranchShell := "git reset --hard && git fetch && git checkout " + baseBranch + " && git pull"
	if detached {
		cmdChangeBranchShell = "git stash && git checkout " + baseBranch
		log.Infof("当前处于游离头状态")
	} else {
		log.Infof("当前不在游离头状态")
	}
	//拉取一下最新的代码
	log.Infof(cmdChangeBranchShell)
	cmdChangeBranch := exec.Command("bash", "-c", cmdChangeBranchShell)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmdChangeBranch.Stdout = &out
	cmdChangeBranch.Stderr = &stderr

	if err = cmdChangeBranch.Run(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "git change branch failed" + err.Error() + stderr.String(),
		})
		return
	}
	log.Infof("git checkout branch done")

	// 运行 gocov convert merged.cov 并将输出传递给 gocov-html

	if branchExist {
		err = os.Remove(inputFile)
		if err != nil {
			fmt.Printf("Error deleting file %s: %v\n", inputFile, err)
		}
		err = os.Remove(outputHTMLFilePath)
		if err != nil {
			fmt.Printf("Error deleting file %s: %v\n", outputHTMLFilePath, err)
		}
		err = os.Remove(reportHTMLFilePath)
		if err != nil {
			fmt.Printf("Error deleting file %s: %v\n", reportHTMLFilePath, err)
		}
	}

	if diffType == "diff" {
		outputHTMLFilePath = "coverage" + extra + ".xml"
	}

	err = os.WriteFile(inputFile, buff.Bytes(), 0644)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "write to cov file failed" + err.Error(),
		})
		return
	}
	// 创建第一个命令 gocov convert coverage.cov
	cmdConvert := exec.Command("gocov", "convert", inputFile)

	// 创建第二个命令 gocov-html
	cmdHTML := exec.Command("gocov-html")
	if diffType == "diff" {
		cmdHTML = exec.Command("gocov-xml")
	}

	// 创建一个管道用于连接两个命令
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	// 将第一个命令的输出设置为管道的写入端
	var convertStderr bytes.Buffer
	cmdConvert.Stdout = writer
	cmdConvert.Stderr = &convertStderr

	// 将第二个命令的输入设置为管道的读取端
	cmdHTML.Stdin = reader

	// 设置第二个命令的输出为文件
	outputHTMLFile, err := os.Create(outputHTMLFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "os.create file failed" + err.Error(),
		})
		return
	}
	defer outputHTMLFile.Close()
	cmdHTML.Stdout = outputHTMLFile
	var cmdHtmlStderr bytes.Buffer
	cmdHTML.Stderr = &cmdHtmlStderr

	// 启动第一个命令
	log.Infof("goc covert and goc-html/xml start")
	if err = cmdConvert.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "gocov convert start failed:" + err.Error() + convertStderr.String(),
		})
		return
	}

	// 启动第二个命令
	if err = cmdHTML.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "gocov-html start failed :" + cmdHtmlStderr.String(),
		})
		return
	}

	// 等待第一个命令完成
	if err = cmdConvert.Wait(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "gocov convert wait failed:" + convertStderr.String(),
		})
		return
	}

	// 关闭管道的写入端，以通知第二个命令没有更多的输入了
	writer.Close()

	// 等待第二个命令完成
	if err = cmdHTML.Wait(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "gocov-html wait failed  " + cmdHtmlStderr.String(),
		})
		return
	}
	log.Infof("goc covert and goc-html/xml done")

	htmlContent, err := ioutil.ReadFile(outputHTMLFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "read file outputhtml file path failed" + err.Error(),
		})
		return
	}

	if diffType == "diff" {
		// 定义要执行的命令和参数
		compareBranch := c.Query("compare")
		if compareBranch == "" {
			compareBranch = "master"
		}
		cmdDIFFCover := exec.Command("diff-cover", outputHTMLFilePath, "--compare-branch="+compareBranch, "--html-report", reportHTMLFilePath)

		// 创建一个新的缓冲变量来存储命令的输出
		cmdDIFFCover.Stdout = &out    // 将命令的标准输出重定向到我们的缓冲变量
		cmdDIFFCover.Stderr = &stderr // 将命令的标准错误输出重定向到我们的缓冲变量（如果需要的话）

		// 执行命令
		log.Infof("diff-cover start...")
		err = cmdDIFFCover.Run()
		if err != nil {
			// 如果有错误，打印到标准错误并返回非零退出码
			c.JSON(http.StatusInternalServerError, gin.H{
				"msg":        "diff-cover run failed " + err.Error() + stderr.String(),
				"annotation": "当前只支持分支对比，不支持commitid",
			})
			return
		}
		log.Infof("diff-cover end")
		htmlContent, err = ioutil.ReadFile(reportHTMLFilePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"msg": "read file report html file path failed" + err.Error(),
			})
			return
		}
	}
	// 将HTML内容发送给HTTP客户端
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, string(htmlContent))
}

func isDetachedHead() (bool, error) {
	// 执行 git status --short --branch 命令
	cmd := exec.Command("git", "status", "--short", "--branch")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return false, err
	}

	// 解析输出
	output := out.String()
	// 通常，如果处于游离头状态，输出会类似于 "## HEAD (detached at <commit-hash>)"
	// 或者没有分支信息（直接显示 commit hash）
	// 我们检查输出是否包含 "HEAD (detached" 来确定是否处于游离头状态
	return strings.Contains(output, "HEAD (detached"), nil
}

func isBranchExist(base_branch string) (bool, error) {
	// 执行 git status --short --branch 命令

	branchExistShell := "git ls-remote origin " + base_branch
	log.Infof("start check branch exist: %s", branchExistShell)
	branchExist := exec.Command("bash", "-c", branchExistShell)
	// 创建一个buffer来保存命令的输出
	var out bytes.Buffer
	branchExist.Stdout = &out

	if err := branchExist.Run(); err != nil {
		return false, err
	}
	output := out.String()
	log.Infof("check branch exist: %s", output)
	if output == "" {
		return false, nil
	}
	return true, nil

}

func readFile(filename string, res *ProfileRes) error {
	if filename == "" {
		return nil
	}
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Errorf("readFile file not found:%v", err)
			return errors.New("File not found")
		} else {
			log.Errorf("readFile file read error:%v", err)
			return errors.New("File read error")
		}
	} else {
		*res = ProfileRes(data)
	}

	return nil
}

func truncFile(filename string) {
	// 打开文件，O_RDWR表示可读可写，O_CREATE表示如果文件不存在则创建，O_TRUNC表示清空文件
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_TRUNC, 0666)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return
	}
	defer file.Close()
}

func saveToFile(filename string, content ProfileRes) error {
	if _, err := os.Stat(filepath.Dir(filename)); os.IsNotExist(err) {
		err := os.MkdirAll(filepath.Dir(filename), 0755)
		if err != nil {
			return err
		}
	}
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	err = ioutil.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		return err
	}

	return nil
}

// getProfiles get and merge all agents' informations
func (gs *gocServer) getMergedProfiles(c *gin.Context) ([]*cover.Profile, error) {
	idQuery := c.Query("id")
	ifInIdMap := idMaps(idQuery)

	skippatternRaw := c.Query("skippattern")
	var skippattern []string
	if skippatternRaw != "" {
		skippattern = strings.Split(skippatternRaw, ",")
	}

	extra := c.Query("extra")
	isExtra := filterExtra(extra)

	var mu sync.Mutex
	var wg sync.WaitGroup

	mergedProfiles := make([][]*cover.Profile, 0)
	var mergedAgentsInfo map[int][]*cover.Profile

	gs.agents.Range(func(key, value interface{}) bool {
		// check if id is in the query ids
		if !ifInIdMap(key.(string)) {
			// not in
			return true
		}

		agent, ok := value.(*gocCoveredAgent)
		if !ok {
			return false
		}

		// check if extra matches
		if !isExtra(agent.Extra) {
			// not match
			return true
		}

		wg.Add(1)
		// 并发 rpc，且每个 rpc 设超时时间 10 second
		go func() {
			defer wg.Done()

			timeout := time.Duration(10 * time.Second)
			done := make(chan error, 1)

			var req ProfileReq = "getprofile"
			var res ProfileRes
			log.Infof("get profile agent :%v", agent)
			go func() {
				// lock-free
				rpc := agent.rpc
				if rpc == nil || agent.Status == DISCONNECT {
					log.Infof("agent:%s is disconnect, get profile from file:%s", agent.Id, agent.FilePath)
					err := readFile(agent.FilePath, &res)
					if err != nil {
						if err.Error() == "File not found" {
							gs.removeAgent(agent.Id)
						}
						log.Errorf("fail to read profile from file: %v, reason: %v. let's close the connection", agent.Id, err)
					}
					done <- nil

				} else if rpc != nil && agent.Status != DISCONNECT {
					err := agent.rpc.Call("GocAgent.GetProfile", req, &res)
					if err != nil {
						log.Errorf("fail to get profile from: %v, reason: %v. let's close the connection", agent.Id, err)
					}
					if err == nil {
						// 如果没有报错，则保存一下最新的覆盖率文件
						log.Infof("agent%s connected, get profile from rpc, and save to file:%s", agent.Id, agent.FilePath)
						err = saveToFile(agent.FilePath, res)
					}
					if err != nil {
						log.Errorf("fail save to file: %v, reason: %v.", agent.Id, err)
					}
					done <- err
				} else {
					log.Errorf("agent:%s get profile fail return", agent.Id)
					done <- nil
					return
				}

			}()

			select {
			// rpc 超时
			case <-time.After(timeout):
				log.Warnf("rpc call timeout: %v", agent.Hostname)
				// 关闭链接
				agent.closeRpcConnOnce()
			case err := <-done:
				// 调用 rpc 发生错误
				if err != nil {
					// 关闭链接
					log.Warnf("rpc call err: %v, close rpc", err)
					agent.closeRpcConnOnce()
				}
			}
			// append profile
			profile, err := convertProfile([]byte(res))
			if err != nil {
				log.Errorf("fail to convert the received profile from: %v, reasson: %v. let's close the connection", agent.Id, err)
				// 关闭链接
				agent.closeRpcConnOnce()
				return
			}

			// check if skippattern matches
			newProfile := filterProfileByPattern(skippattern, profile)

			mu.Lock()
			defer mu.Unlock()
			log.Infof("append merge profiles by agent.id:%s", agent.Id)
			agentId, err := strconv.Atoi(agent.Id)
			if err != nil {
				log.Errorf("fail to convert agent id to int: %v, reason: %v", agent.Id, err)
			}
			mergedAgentsInfo[agentId] = newProfile
			mergedProfiles = append(mergedProfiles, newProfile)
		}()

		return true
	})

	// 一直等待并发的 rpc 都回应
	wg.Wait()
	log.Infof("start cov merge multiple profiles, count:%d", len(mergedProfiles))
	merged, err := cov.MergeMultipleProfiles(mergedProfiles)
	if err != nil {
		log.Errorf("merge multiple profiles error: %v", err)
		log.Infof(" merged agents info:%v", mergedAgentsInfo)
		// 将map的key值存储到slice中
		keys := make([]int, 0, len(mergedAgentsInfo))
		for k := range mergedAgentsInfo {
			keys = append(keys, k)
		}

		// 对slice进行排序
		sort.Ints(keys)

		// 获取最大的key值
		maxKey := keys[len(keys)-1]
		log.Infof("Max key:", maxKey)
		mergedProfiles = mergedProfiles[:0]
		mergedProfiles = append(mergedProfiles, mergedAgentsInfo[maxKey])
		merged, err = cov.MergeMultipleProfiles(mergedProfiles)
		if err != nil {
			return merged, err
		}
	}

	return merged, err
}

// it is synchronous
func (gs *gocServer) getProfiles(c *gin.Context) {
	merged, err := gs.getMergedProfiles(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": err.Error(),
		})
		return
	}

	var buff bytes.Buffer
	err = cov.DumpProfile(merged, &buff)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"profile": buff.String(),
	})
}

// resetProfiles reset all profiles in agent
//
// it is async, the function will return immediately
func (gs *gocServer) resetProfiles(c *gin.Context) {
	idQuery := c.Query("id")
	ifInIdMap := idMaps(idQuery)

	extra := c.Query("extra")
	isExtra := filterExtra(extra)

	gs.agents.Range(func(key, value interface{}) bool {

		// check if id is in the query ids
		if !ifInIdMap(key.(string)) {
			// not in
			return true
		}

		agent, ok := value.(*gocCoveredAgent)
		if !ok {
			return false
		}

		// check if extra matches
		if !isExtra(agent.Extra) {
			// not match
			return true
		}

		var req ProfileReq = "resetprofile"
		var res ProfileRes
		go func() {
			// lock-free
			rpc := agent.rpc
			if rpc == nil || agent.Status == DISCONNECT {
				truncFile(agent.FilePath)
				return
			}
			err := rpc.Call("GocAgent.ResetProfile", req, &res)
			truncFile(agent.FilePath)
			if err != nil {
				log.Errorf("fail to reset profile from: %v, reasson: %v. let's close the connection", agent.Id, err)
				// 关闭链接
				agent.closeRpcConnOnce()
			}
		}()

		return true
	})
}

// watchProfileUpdate watch the profile change
//
// any profile change will be updated on this websocket connection.
func (gs *gocServer) watchProfileUpdate(c *gin.Context) {
	// upgrade to websocket
	ws, err := gs.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("fail to establish websocket connection with watch client: %v", err)
		c.JSON(http.StatusInternalServerError, nil)
	}

	log.Infof("watch client connected")

	id := time.Now().String()
	gwc := &gocWatchClient{
		ws:     ws,
		exitCh: make(chan int),
	}
	gs.watchClients.Store(id, gwc)
	// send close msg and close ws connection
	defer func() {
		gs.watchClients.Delete(id)
		ws.Close()
		gwc.once.Do(func() { close(gwc.exitCh) })
		log.Infof("watch client disconnected")
	}()

	// set pong handler
	ws.SetReadDeadline(time.Now().Add(PongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	// set ping goroutine to ping every PingWait time
	go func() {
		ticker := time.NewTicker(PingWait)
		defer ticker.Stop()

		for range ticker.C {
			if err := gs.wsping(ws, PongWait); err != nil {
				break
			}
		}

		gwc.once.Do(func() { close(gwc.exitCh) })
	}()

	<-gwc.exitCh
}

func filterProfileByPattern(skippattern []string, profiles []*cover.Profile) []*cover.Profile {

	if len(skippattern) == 0 {
		return profiles
	}

	var out = make([]*cover.Profile, 0)
	for _, profile := range profiles {
		skip := false
		for _, pattern := range skippattern {
			if strings.Contains(profile.FileName, pattern) {
				skip = true
				break
			}
		}

		if !skip {
			out = append(out, profile)
		}
	}

	return out
}

func idMaps(idQuery string) func(key string) bool {
	idMap := make(map[string]bool)
	if len(strings.TrimSpace(idQuery)) == 0 {
	} else {
		ids := strings.Split(idQuery, ",")
		for _, id := range ids {
			idMap[id] = true
		}
	}

	inIdMaps := func(key string) bool {
		// if no id in query, then all id agent will be return
		if len(idMap) == 0 {
			return true
		}
		// other
		_, ok := idMap[key]
		if !ok {
			return false
		} else {
			return true
		}
	}

	return inIdMaps
}

func filterExtra(extraPattern string) func(string) bool {

	re := regexp.MustCompile(extraPattern)

	return func(extra string) bool {
		return re.Match([]byte(extra))
	}
}
