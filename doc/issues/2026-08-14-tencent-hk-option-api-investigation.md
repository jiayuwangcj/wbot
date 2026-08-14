# 腾讯港股期权历史接口调查（2026-08-14）

## 结论

截至 2026-08-14，**腾讯免费行情接口不能作为港股期权历史数据源接入**。已上市的腾讯控股期权合约可以从 Futu/HKEX 侧确认，但腾讯 `qt.gtimg.cn` 不识别其合约代码，`fqkline` 对同一代码返回空日 K；也未找到腾讯公开的港股期权合约发现/代码映射接口。因此本任务只回填标的 `HK.00700` 日 K，期权面继续由实时 `option_quote_snapshots` 向未来积累，不能用当前快照或期权 OHLC 伪造历史原子 snapshot。

这是一次针对已知代码格式和当前免费端点的可复现实测结论，不声称腾讯内部不存在未公开接口。若将来取得腾讯正式文档或可稳定发现合约的接口，须另开任务重新做字段、原子性、限频和历史覆盖验收。

## 合约真实性基准

- Futu 当前链（只读，`HK.00700`）在 2026-08-14 返回合约，例如 `HK.TCH260828P450000`；`TCH` 是腾讯控股的 HKATS code。
- 香港交易所公开资料也列出腾讯控股：SEHK code `700`、HKATS code `TCH`、合约规模 100 股。参见 [HKEX Stock Options](https://www.hkex.com.hk/Products/Listed-Derivatives/Single-Stock/Stock-Options?sc_lang=en) 和 [Tencent option detail example](https://www.hkex.com.hk/eng/sorc/options/stock_options_detail.aspx?oID=1391940&ucode=00700)。

## 腾讯端点实测

每个请求间隔超过 1 秒。候选合约使用尚未到期的 `TCH260828P450000`，并覆盖腾讯港股常见的 `hk` / `r_hk` 前缀与裸代码：

| 端点 / 参数 | 结果 | 判定 |
| --- | --- | --- |
| `https://qt.gtimg.cn/q=hkTCH260828P450000` | `v_pv_none_match="1";` | 不识别合约 |
| `https://qt.gtimg.cn/q=r_hkTCH260828P450000` | `v_pv_none_match="1";` | 不识别合约 |
| `https://qt.gtimg.cn/q=TCH260828P450000` | `v_pv_none_match="1";` | 不识别合约 |
| `https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=hkTCH260828P450000,day,,,20,qfq` | `code=0`，但 `day=[]`、`qt` 为空 | 无历史 K |
| 同一 `fqkline` 端点，裸 `TCH260828P450000` | `code=0,msg="param error"` | 参数格式不支持 |

对照请求 `param=hk00700,day,,,1000,qfq` 同时返回 1001 条标的日 K，说明上述空结果不是端点整体不可达。

## 产品边界

- 不实现腾讯港股期权回填 adapter。
- 不把 HKEX 延迟详情、Futu 当前 Greeks、当前 bid/ask 或腾讯标的日 K 拼成历史 snapshot。
- `data_quality.option_snapshot_sources` 只报告回测实际消费的实时积累来源；没有历史 snapshot 时继续 `DATA_BLOCKED/HOLD`。
- 解锁条件：存在可发现合约、可按过去时点获取完整 bid/ask/Delta/IV/Theta/OI/volume/lot size、能证明同一时点原子性，并覆盖完整到期周期的供应商接口。
