# 闭环 #65: verify.sh 补齐 gofmt/race/staticcheck(≡ CI test job)

- **日期**: 2026-08-03
- **PR**: #248(功能)+ 本文档(归档)
- **背景**: AUTO_ADVANCE triage 对账维度「声明 vs 实际实现」——README.md:32(#60 写入)声称 verify.sh「单测 + gofmt + race/staticcheck + 零依赖 accept(≡ CI test job)」,但脚本实际只有 test/vet + CLI smoke + 两个零依赖 accept;git 溯源确认 verify.sh 从未含 gofmt/race/staticcheck。

## 改动

- scripts/verify.sh:补 gofmt 检查(与 ci.yml "Check gofmt" 同命令 `git ls-files '*.go' | gofmt -l`)、`go test -race ./... -count=1`(同 "Run race tests")、staticcheck(同 "Run staticcheck";未安装时打印安装提示并 exit 1,门禁不静默降级);头部注释改为「≡ CI test job」契约
- 本机安装 staticcheck 2026.1(v0.7.0,与 CI 同 @latest)

## 验证

- 本地 `scripts/verify.sh` 全量通过(gofmt/test/vet/race/staticcheck/CLI smoke/accept-paper/accept-agent-federation);CI 全量(test 1m51s + db-integration 1m33s + governance)5/5

## 备注

- **引擎经验**: 新对账维度——「README/文档的『≡』等价声明 vs 脚本实际命令」:声明等价就必须逐步骤比对两边的命令清单;本次修复选择「升级脚本使声明为真」(而非弱化声明),符合「本地全可用才提交」验收规则——本地门禁必须能抓住 CI 会抓的一切。
- **候选池**: 仍枯竭(待老板 7 项)。
