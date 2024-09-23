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

package log

import (
	"go.uber.org/zap/zapcore"
	"time"
)

var g Logger

func NewLogger(debug bool) {
	if debug == true {
		g = newCiLogger()
	} else {
		g = &terminalLogger{
			level: zapcore.InfoLevel,
		}
	}
}

func Debugf(format string, args ...interface{}) {
	g.Debugf(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func Donef(format string, args ...interface{}) {
	g.Donef(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func Infof(format string, args ...interface{}) {
	g.Infof(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func Warnf(format string, args ...interface{}) {
	g.Warnf(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func Fatalf(format string, args ...interface{}) {
	g.Fatalf(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func Errorf(format string, args ...interface{}) {
	g.Errorf(time.Now().Format("2006-01-02 15:04:05")+format, args...)
}

func StartWait(message string) {
	g.StartWait(message)
}

func StopWait() {
	g.StopWait()
}

func Sync() {
	g.Sync()
}
