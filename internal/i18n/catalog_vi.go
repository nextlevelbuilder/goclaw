package i18n

func init() {
	register(LocaleVI, map[string]string{
		// Common validation
		MsgRequired:         "%s là bắt buộc",
		MsgInvalidID:        "ID %s không hợp lệ",
		MsgNotFound:         "không tìm thấy %s: %s",
		MsgAlreadyExists:    "%s đã tồn tại: %s",
		MsgInvalidRequest:   "yêu cầu không hợp lệ: %s",
		MsgInvalidJSON:      "JSON không hợp lệ",
		MsgUnauthorized:     "chưa xác thực",
		MsgPermissionDenied: "từ chối quyền truy cập: %s",
		MsgInternalError:    "lỗi nội bộ: %s",
		MsgInvalidSlug:      "%s phải là slug hợp lệ (chữ thường, số, dấu gạch ngang)",
		MsgFailedToList:     "không thể liệt kê %s",
		MsgFailedToCreate:   "không thể tạo %s: %s",
		MsgFailedToUpdate:   "không thể cập nhật %s: %s",
		MsgFailedToDelete:   "không thể xóa %s: %s",
		MsgFailedToSave:     "không thể lưu %s: %s",
		MsgInvalidUpdates:   "cập nhật không hợp lệ",

		// Agent
		MsgAgentNotFound:       "không tìm thấy agent: %s",
		MsgCannotDeleteDefault: "không thể xóa agent mặc định",
		MsgUserCtxRequired:     "yêu cầu ngữ cảnh người dùng",

		// Chat
		MsgRateLimitExceeded: "vượt quá giới hạn tốc độ — vui lòng đợi",
		MsgNoUserMessage:     "không tìm thấy tin nhắn người dùng",
		MsgUserIDRequired:    "user_id là bắt buộc",
		MsgMsgRequired:       "tin nhắn là bắt buộc",

		// Channel instances
		MsgInvalidChannelType: "loại channel không hợp lệ",
		MsgInstanceNotFound:   "không tìm thấy phiên bản",

		// Cron
		MsgJobNotFound:     "không tìm thấy tác vụ",
		MsgInvalidCronExpr: "biểu thức cron không hợp lệ: %s",

		// Config
		MsgConfigHashMismatch: "cấu hình đã thay đổi (hash không khớp)",

		// Exec approval
		MsgExecApprovalDisabled: "phê duyệt thực thi chưa được bật",

		// Pairing
		MsgSenderChannelRequired: "senderId và channel là bắt buộc",
		MsgCodeRequired:          "mã là bắt buộc",
		MsgSenderIDRequired:      "sender_id là bắt buộc",

		// HTTP API
		MsgInvalidAuth:           "xác thực không hợp lệ",
		MsgMsgsRequired:          "messages là bắt buộc",
		MsgUserIDHeader:          "header X-GoClaw-User-Id là bắt buộc",
		MsgFileTooLarge:          "tệp quá lớn hoặc form multipart không hợp lệ",
		MsgMissingFileField:      "thiếu trường 'file'",
		MsgInvalidFilename:       "tên tệp không hợp lệ",
		MsgChannelKeyReq:         "channel và key là bắt buộc",
		MsgMethodNotAllowed:      "phương thức không được phép",
		MsgStreamingNotSupported: "streaming không được hỗ trợ",
		MsgOwnerOnly:             "chỉ chủ sở hữu mới có thể %s",
		MsgNoAccess:              "không có quyền truy cập %s này",
		MsgAlreadySummoning:      "agent đang được triệu hồi",
		MsgSummoningUnavailable:  "triệu hồi không khả dụng",
		MsgNoDescription:         "agent không có mô tả để triệu hồi lại",
		MsgInvalidPath:           "đường dẫn không hợp lệ",

		// Scheduler
		MsgQueueFull:    "hàng đợi session đã đầy",
		MsgShuttingDown: "cổng đang tắt, vui lòng thử lại sau",

		// Provider
		MsgProviderReqFailed: "%s: yêu cầu thất bại: %s",

		// Unknown method
		MsgUnknownMethod: "phương thức không xác định: %s",

		// Not implemented
		MsgNotImplemented: "%s chưa được triển khai",

		// Agent links
		MsgLinksNotConfigured:   "liên kết agent chưa được cấu hình",
		MsgInvalidDirection:     "hướng phải là outbound, inbound hoặc bidirectional",
		MsgSourceTargetSame:     "nguồn và đích phải là các agent khác nhau",
		MsgCannotDelegateOpen:   "không thể ủy quyền cho agent mở — chỉ agent định sẵn mới có thể là đích ủy quyền",
		MsgNoUpdatesProvided:    "không có cập nhật nào được cung cấp",
		MsgInvalidLinkStatus:    "trạng thái phải là active hoặc disabled",

		// Teams
		MsgTeamsNotConfigured:   "nhóm chưa được cấu hình",
		MsgAgentIsTeamLead:      "agent đã là trưởng nhóm",
		MsgCannotRemoveTeamLead: "không thể xóa trưởng nhóm",

		// Channels
		MsgCannotDeleteDefaultInst: "không thể xóa phiên bản channel mặc định",
		MsgCannotRemoveLastWriter:  "không thể xóa người quản lý cuối cùng",

		// Skills
		MsgSkillsUpdateNotSupported: "skills.update không được hỗ trợ với skill dựa trên tệp",
		MsgCannotResolveSkillID:     "không thể xác định ID skill dựa trên tệp",

		// Logs
		MsgInvalidLogAction: "action phải là 'start' hoặc 'stop'",

		// Config
		MsgRawConfigRequired:     "cấu hình raw là bắt buộc",
		MsgRawPatchRequired:      "patch raw là bắt buộc",
		MsgConfigMasterScopeOnly: "config.* chỉ áp dụng cho master scope; dùng endpoint tenant tool config cho override theo tenant",
		MsgMasterScopeRequired:   "thao tác này yêu cầu phạm vi tenant chính",

		// Storage / File
		MsgCannotDeleteSkillsDir: "không thể xóa thư mục skill",
		MsgFailedToReadFile:      "không thể đọc tệp",
		MsgFileNotFound:          "không tìm thấy tệp",
		MsgInvalidVersion:        "phiên bản không hợp lệ",
		MsgVersionNotFound:       "không tìm thấy phiên bản",
		MsgFailedToDeleteFile:    "không thể xóa",

		// OAuth
		MsgNoPendingOAuth:    "không có luồng OAuth đang chờ",
		MsgFailedToSaveToken: "không thể lưu token",

		// Intent Classify
		MsgStatusWorking:       "🔄 Mình đang xử lý yêu cầu của bạn... Vui lòng chờ.",
		MsgStatusDetailed:      "🔄 Mình đang xử lý yêu cầu của bạn...\n%s (lần lặp %d)\nĐã chạy: %s\n\nVui lòng chờ — mình sẽ phản hồi khi xong.",
		MsgStatusPhaseThinking: "Giai đoạn: Đang suy nghĩ...",
		MsgStatusPhaseToolExec: "Giai đoạn: Đang chạy %s",
		MsgStatusPhaseTools:    "Giai đoạn: Đang thực thi công cụ...",
		MsgStatusPhaseCompact:  "Giai đoạn: Đang nén ngữ cảnh...",
		MsgStatusPhaseDefault:  "Giai đoạn: Đang xử lý...",
		MsgCancelledReply:      "✋ Đã hủy. Bạn muốn làm gì tiếp?",
		MsgInjectedAck:         "Đã nhận, tôi sẽ xử lý trong tác vụ hiện tại.",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_id là bắt buộc",
		MsgEntityFieldsRequired:   "external_id, name và entity_type là bắt buộc",
		MsgTextRequired:           "text là bắt buộc",
		MsgProviderModelRequired:  "provider và model là bắt buộc",
		MsgInvalidProviderOrModel: "provider hoặc model không hợp lệ",

		// Mô tả công cụ tích hợp
		MsgToolReadFile:        "Đọc nội dung tệp từ workspace của agent theo đường dẫn",
		MsgToolWriteFile:       "Ghi nội dung vào tệp trong workspace, tự động tạo thư mục nếu cần",
		MsgToolListFiles:       "Liệt kê tệp và thư mục trong đường dẫn chỉ định",
		MsgToolEdit:            "Chỉnh sửa tệp bằng cách tìm và thay thế đoạn văn bản cụ thể",
		MsgToolExec:            "Thực thi lệnh shell trong workspace và trả về kết quả",
		MsgToolWebSearch:       "Tìm kiếm thông tin trên web bằng công cụ tìm kiếm (Brave hoặc DuckDuckGo)",
		MsgToolWebFetch:        "Tải trang web hoặc API endpoint và trích xuất nội dung văn bản",
		MsgToolMemorySearch:    "Tìm kiếm trong bộ nhớ dài hạn của agent bằng độ tương đồng ngữ nghĩa",
		MsgToolMemoryGet:       "Lấy tài liệu bộ nhớ cụ thể theo đường dẫn tệp",
		MsgToolKGSearch:        "Tìm kiếm thực thể, quan hệ và ghi chú trong đồ thị tri thức của agent",
		MsgToolReadImage:       "Phân tích hình ảnh bằng nhà cung cấp LLM có khả năng nhìn",
		MsgToolReadDocument:    "Phân tích tài liệu (PDF, Word, Excel, PowerPoint, CSV, v.v.) bằng LLM",
		MsgToolCreateImage:     "Tạo hình ảnh từ mô tả văn bản bằng nhà cung cấp tạo ảnh AI",
		MsgToolReadAudio:       "Phân tích tệp âm thanh (giọng nói, nhạc, âm thanh) bằng LLM",
		MsgToolReadVideo:       "Phân tích tệp video bằng nhà cung cấp LLM có khả năng xử lý video",
		MsgToolCreateVideo:     "Tạo video từ mô tả văn bản bằng AI",
		MsgToolCreateAudio:     "Tạo nhạc hoặc hiệu ứng âm thanh từ mô tả văn bản bằng AI",
		MsgToolTTS:             "Chuyển văn bản thành giọng nói tự nhiên",
		MsgToolBrowser:         "Tự động hóa trình duyệt: điều hướng trang, click, điền form, chụp ảnh màn hình",
		MsgToolSessionsList:    "Liệt kê các phiên chat đang hoạt động trên tất cả kênh",
		MsgToolSessionStatus:   "Xem trạng thái và thông tin chi tiết của một phiên chat",
		MsgToolSessionsHistory: "Xem lịch sử tin nhắn của một phiên chat cụ thể",
		MsgToolSessionsSend:    "Gửi tin nhắn vào một phiên chat đang hoạt động thay mặt agent",
		MsgToolMessage:         "Gửi tin nhắn chủ động đến người dùng trên kênh đã kết nối (Telegram, Discord, v.v.)",
		MsgToolCron:            "Lên lịch hoặc quản lý tác vụ định kỳ bằng biểu thức cron, giờ cố định, hoặc khoảng thời gian",
		MsgToolSpawn:           "Tạo subagent chạy nền hoặc giao việc cho agent đã liên kết",
		MsgToolSkillSearch:     "Tìm kiếm kỹ năng có sẵn theo từ khóa hoặc mô tả",
		MsgToolUseSkill:        "Kích hoạt kỹ năng để sử dụng khả năng chuyên biệt (đánh dấu tracing)",
		MsgToolSkillManage:     "Tạo, sửa hoặc xóa kỹ năng từ trải nghiệm hội thoại",
		MsgToolPublishSkill:    "Đăng ký thư mục kỹ năng vào hệ thống, cho phép tìm kiếm và cấp quyền",
		MsgToolTeamTasks:       "Xem, tạo, cập nhật và hoàn thành tác vụ trên bảng tác vụ nhóm",

		MsgSkillNudgePostscript: "Tác vụ này cần nhiều bước. Bạn muốn tôi lưu quy trình này thành kỹ năng tái sử dụng không? Trả lời **\"lưu kỹ năng\"** hoặc **\"bỏ qua\"**.",
		MsgSkillNudge70Pct:      "[System] Bạn đã dùng 70% ngân sách vòng lặp. Cân nhắc xem các mẫu trong phiên này có nên lưu thành kỹ năng không.",
		MsgSkillNudge90Pct:      "[System] Bạn đã dùng 90% ngân sách vòng lặp. Nếu phiên này có quy trình tái sử dụng, hãy cân nhắc lưu thành kỹ năng trước khi hoàn thành.",

		MsgInvalidRole: "vai trò không hợp lệ: giá trị cho phép là owner, admin, operator, member, viewer",

		MsgContactIDsRequired:  "contact_ids là bắt buộc",
		MsgMergeTargetRequired: "cần chính xác một trong tenant_user_id hoặc create_user",
		MsgTenantUserNotFound:  "không tìm thấy tenant user",
		MsgTenantMismatch:      "tenant user không thuộc tenant này",
		MsgTenantScopeRequired: "cần xác định tenant để thực hiện thao tác này",
	})
}
