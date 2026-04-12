# Vi Sao Session Chat `fe071214` Tu Reset Va Mat Tin Nhan

Session `agent:coder:ws:direct:fe071214-4f63-4e71-9dcd-00cad71d09e3` khong bi "mang yeu" theo nghia thong thuong. Vấn đề chinh nam o backend: trong luc agent dang stream tool-call, process GoClaw bi panic, container bi restart, va browser buoc phai reconnect. Tu goc nhin nguoi dung, no giong nhu trang tu reload sau vai giay.

Tai sao tin nhan cua user bien mat? Vi o phien ban truoc, session row da duoc tao trong Postgres, nhung user message moi chi song trong session cache cua process. Khi process crash truoc buoc `Save()`, DB van con session key do, nhung `messages=[]`. Sau khi browser vao lai, UI doc dung su that tu server: session ton tai, nhung khong con lich su nao de hien.

## System picture

Hay hinh dung he thong nay nhu 4 lop:

1. Browser GoClaw UI gui message va giu websocket voi gateway.
2. Gateway mo mot run cho agent `coder`, build system prompt, nap history, roi goi LLM theo che do stream.
3. Trong luc LLM stream ra text va tool-call, `StreamingToolExecutor` co gang chay tool som de tiet kiem thoi gian.
4. Session store ghi lich su vao Postgres de lan sau mo lai van con hoi thoai.

Trong case nay, lop 3 va lop 4 gap nhau o diem xau nhat:

- Lop 3 panic trong luong streaming tool executor.
- Lop 4 chua kip flush user message xuong DB.

Ket qua la websocket roi, process restart, va session tro thanh "co key nhung rong noi dung".

## Main flow

Timeline quan sat duoc tu log va DB:

1. `2026-04-11 14:02:54Z` (`21:02:54` gio Viet Nam): gateway resolve agent `coder`, model `claude-opus-4.6`, provider `claude-opus-4-6-app-quick-link`.
2. Ngay sau do, he thong build xong system prompt va bat dau vong streaming.
3. Trong giai doan stream tool-call, backend gap panic:

   `panic: sync: WaitGroup is reused before previous Wait has returned`

   Diem roi stack la `internal/pipeline/streaming_tool_executor.go`.
4. Process chet, container restart vao khoang `2026-04-11 14:03:04Z` (`21:03:04` gio Viet Nam).
5. Khi kiem tra session row trong Postgres, session `fe071214...` ton tai nhung co:
   - `msg_count = 0`
   - `model = ''`
   - `provider = ''`
6. Khi user mo lai URL session, UI khong "lam mat" tin nhan; UI chi render dung du lieu rong ma server con giu.

Flow sau khi fix o ban `codex-upgrade-cps-prod-loop94`:

1. `StreamingToolExecutor` khong con dung `WaitGroup.Wait()` theo kieu co the bi `Add()` tro lai khi stream sap dong.
2. Khi stream da ket thuc, tool-call den muon se khong chen vao executor nua; chung se duoc xu ly o nhanh tool-call con lai cua response cuoi.
3. User input snapshot duoc persist som hon:
   - ghi `agent_id`, `user_id`
   - ghi `model`, `provider`, `channel`
   - ghi user message
   - goi `Save()` truoc khi vao pha LLM/tool execution nguy hiem
4. Neu co loi xay ra sau do, session van con toi thieu phan lich su vua gui.

Mot cach nhin sequence de hieu:

| Buoc | Browser | Gateway | Streaming Executor | Postgres |
| --- | --- | --- | --- | --- |
| 1 | Gui message | Tao run, load context | Chua chay | Session row duoc tao |
| 2 | Doi chunk | Goi LLM stream | Nhan tool-call chunk | Chua co user message neu chua save |
| 3 | Mat ket noi | Process panic | Crash giua stream | Row ton tai nhung `messages=[]` |
| 4 | Reconnect | Container moi len lai | Trang thai cu mat | UI doc du lieu rong |
| 5 | Gui message moi tren `loop94` | Persist snapshot som | Stream an toan hon | Session co `messages` ngay tu dau |

## Technical terms

`StreamingToolExecutor`

La bo phan chay tool ngay khi LLM moi stream ra tool-call, khong doi den luc toan bo response ket thuc.

`WaitGroup reuse panic`

Day la loi dong bo cua Go. No xay ra khi mot goroutine dang `Wait()` nhung mot noi khac lai tiep tuc `Add()` cung `WaitGroup`. Trong case nay, day la bug app, khong phai loi ha tang.

`Session snapshot`

La trang thai toi thieu can duoc luu som de du co crash thi van con session, nguoi gui, model/provider, va user message vua nhap.

`Reconnect`

Browser websocket bi dong thi client se ket noi lai. Neu process server vua crash va vua len lai, nguoi dung cam thay nhu "trang tu refresh".

## What this means in practice

- Nguyen nhan goc cua session `fe071214...` la bug ung dung, khong phai do SSH server, Cloudflare, hay domain public bi yeu.
- Tin nhan cu cua session do khong the "hoi sinh" vi no chua bao gio duoc persist truoc luc crash. UI rong la ket qua trung thuc cua DB.
- Production da duoc va o ban `codex-upgrade-cps-prod-loop94`.
- Verify sau khi va:
  - container production `restart=0`, `health=healthy`
  - public `GET /health` tra `{\"status\":\"ok\",\"protocol\":3}`
  - session moi `agent:coder:ws:direct:9b3e6542-48b3-44a5-a82d-01674e1b4164` duoc tao thanh cong
  - session moi nay co `msg_count = 2`, `model = claude-opus-4.6`, `provider = claude-opus-4-6-app-quick-link`, `channel = ws`
  - sau khi reload trang, ca user message va assistant reply van con

Takeaway ngan gon:

- "Trang tu reload" la bieu hien be ngoai cua mot backend panic.
- "Mat tin nhan" la do persist qua muon, khong phai do UI tu xoa.
- Ban `loop94` da sua ca hai dau: chong panic va persist som de crash-safe hon.
