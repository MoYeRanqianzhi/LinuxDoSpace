# LinuxDoSpace 更新日志

## 0.5.3-alpha.3

- 临时收紧分发策略，仅允许登录用户申请、查看和管理与自己 Linux Do 用户名同名的子域名。
- 临时收紧 DNS 管理策略，仅允许编辑 `<username>.<root_domain>` 这条根记录，拒绝 `www`、`api` 等额外子记录。
- 登录后若当前同名子域还没有真实解析记录，前端会先展示一条未填写内容的占位记录，方便用户直接补全。

## 0.5.3-alpha.2

- 修复部署环境使用错误 DNS 解析 `connect.linux.do` 的问题，为 Docker Compose 明确指定公共 DNS。
- 解决 Linux Do OAuth token 请求因上游域名解析错误而超时，避免登录回调阶段返回 `502`。

## 0.5.3-alpha.1

- 参考 `QuantumNous/new-api` 修复 Linux Do OAuth token 交换方式，改为使用 HTTP Basic Auth 传递 `client_id:client_secret`。
- 为 Linux Do OAuth 回调失败增加后端日志，便于定位线上 `502` 或鉴权失败根因。
- 在不改变现有登录页布局的前提下，为 Linux Do 登录按钮接入品牌 SVG 图标并更新按钮文案。

## 0.5.2-alpha.1

- 修复 Linux Do OAuth 客户端未显式发送 Accept: application/json 的问题。
- 修复部署默认值把 LINUXDO_OAUTH_SCOPE 留空的问题，统一改为 user。
- 增加 Linux Do OAuth 客户端测试，覆盖授权 URL、token 交换和用户信息请求。

## 0.5.1-alpha.1

- 淇 GitHub Actions 宸ヤ綔娴佽娉曢敊璇€?- 閬垮厤鍦?job 绾?`if` 鏉′欢涓洿鎺ュ紩鐢?`secrets.*`锛屾敼涓轰粎鍒ゆ柇鎵嬪姩閮ㄧ讲杈撳叆銆?- 澧炲姞閮ㄧ讲 job 鍐呴儴鐨?secret 鏍￠獙姝ラ锛岀‘淇濈己澶遍厤缃椂鏄庣‘澶辫触銆?
## 0.5.0-alpha.1

- 澧炲姞鍗曢暅鍍?Docker 閮ㄧ讲鏂规锛屽墠绔瀯寤轰骇鐗╀細宓屽叆 Go 浜岃繘鍒躲€?- 澧炲姞鏍圭洰褰?`Dockerfile` 涓?`.dockerignore`銆?- 澧炲姞 Debian 鏈嶅姟鍣ㄤ娇鐢ㄧ殑 `docker-compose.yml` 涓庣幆澧冨彉閲忔ā鏉裤€?- 澧炲姞 GitHub Actions 瀹瑰櫒鏋勫缓銆丟HCR 鍙戝竷涓庡彲閫?Debian SSH 閮ㄧ讲宸ヤ綔娴併€?- 琛ュ厖閮ㄧ讲鏂囨。銆佽繍琛屾墜鍐屽拰鍙戝竷璇存槑銆?
## 0.4.1-alpha.1

- 淇 `Agents.md` 琚敊璇彁浜ゅ埌浠撳簱鐨勯棶棰樸€?- 鍦?`.gitignore` 涓鍔?`Agents.md` 涓?`AGENTS.md` 蹇界暐瑙勫垯銆?- 灏嗗凡璺熻釜鐨?`Agents.md` 浠?Git 绱㈠紩绉婚櫎锛屼絾淇濈暀鏈湴鏂囦欢銆?
## 0.1.0-alpha.1

- 鍒濆鍖?Git 浠撳簱銆?- 寤虹珛 Go 鍚庣鍩虹楠ㄦ灦銆?- 澧炲姞閰嶇疆鍔犺浇銆丼QLite 鍒濆鍖栧拰 SQL 杩佺Щ銆?- 澧炲姞 Linux Do / Cloudflare 瀹㈡埛绔垵鐗堛€?- 澧炲姞 `GET /healthz` 鍋ュ悍妫€鏌ユ帴鍙ｃ€?- 寤虹珛寮€鍙戞枃妗ｇ洰褰曚笌鍩虹鏂囨。銆?
## 0.2.0-alpha.1

- 澧炲姞 Linux Do OAuth 鐧诲綍娴佺▼銆佷細璇濆垱寤哄拰閫€鍑虹櫥褰曘€?- 澧炲姞鏈嶅姟绔?Session銆丆SRF 鏍￠獙鍜?User-Agent 鎸囩汗缁戝畾銆?- 澧炲姞鏍瑰煙鍚嶉厤缃€佺敤鎴烽厤棰濊鐩栧拰鍛藉悕绌洪棿鍒嗛厤鑳藉姏銆?- 澧炲姞 Cloudflare 瀹炴椂 DNS 璁板綍鍒涘缓銆佹煡璇€佹洿鏂板拰鍒犻櫎銆?- 澧炲姞绠＄悊鍛樻帴鍙ｅ拰瀹¤鏃ュ織鍐欏叆銆?- 澧炲姞鍗曞厓娴嬭瘯涓?Cloudflare 鐪熷疄闆嗘垚娴嬭瘯銆?
## 0.4.0-alpha.1

- 鍓嶇鎺ュ叆鍚庣鐪熷疄 API锛屼笉鍐嶄娇鐢ㄩ殢鏈哄崰鐢ㄧ姸鎬佸拰鏈湴 mock 璁板綍銆?- 澧炲姞鍓嶇缁熶竴 API 瀹㈡埛绔€佺被鍨嬪畾涔夊拰鐜鍙橀噺閰嶇疆銆?- 澧炲姞鍓嶇鐧诲綍鎬佸悓姝ャ€丱Auth 璺宠浆鍜?URL 涓?tab 鐘舵€佸悓姝ャ€?- 澧炲姞鍓嶇 allocation 鐢宠銆丏NS 璁板綍鏌ヨ銆佸垱寤恒€佹洿鏂板拰鍒犻櫎鑳藉姏銆?- 鍦ㄤ繚鐣欏師鏈?UI 璁捐椋庢牸鐨勫墠鎻愪笅瀹屾垚鐪熷疄涓氬姟鑱旇皟銆?

