# 摘要 - 设计原则

goc 的定位是一个专注提升测试体验和项目质量的工具，它不用于生产环境。

当用户从 go 切换为 goc 时，**成本应越小越好**。如果是生产环境无法替代的工具，那使用部署再怎么不便，用户也会趋之若鹜。

因此 v2 版本在如下：

1. goc 命令行使用
2. goc 部署方式（即 agent <-> server 通信方式）

做了大量重构甚至重写。

得益于重写的通信方式，v2 还提供了 watch 模式，为第三方开发自己的实时代码染色系统提供了接口。