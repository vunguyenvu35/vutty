# Kubernetes trong Panel: so sánh đối thủ và hướng đi

Ngày: 2026-11-05. Phần đối thủ lấy từ kiến thức chung, chưa kiểm lại phiên bản mới nhất.

## Vị trí

Vutty không cạnh tranh về độ rộng với Lens hay Rancher. Lợi thế là K8s nằm chung app với SSH, SFTP, Docker, Panel; đi được qua SSH/bastion mà không cài gì lên server. Không đối thủ nào làm được cả hai.

## So sánh nhanh

| | Vutty | Lens | k9s | Headlamp | Portainer | Rancher |
|---|---|---|---|---|---|---|
| Dạng | Desktop | Desktop | TUI | Web/Desktop | Web | Web |
| Cài lên server | Không | Không | Không | Có (in-cluster) | Agent | Có |
| Qua SSH/bastion | ✅ | ❌ | ◐ | ❌ | ❌ | ❌ |
| Chỉ cần kubeconfig | ✅ | ✅ | ✅ | ✅ | ◐ | ◐ |
| Pod, workload, service, node, event, log | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Shell, scale, restart, rollback, drain | ✅ | ✅ | ✅ | ✅ | ◐ | ✅ |
| Port-forward | ◐ | ✅ | ✅ | ✅ | ❌ | ❌ |
| CRD / resource tuỳ ý | ❌ | ✅ | ✅ | ✅ | ◐ | ✅ |
| Sửa YAML / apply | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Helm | ❌ | ✅ | ❌ | plugin | ✅ | ✅ |
| Metrics có lịch sử | ◐ | ✅ | ◐ | plugin | ◐ | ✅ |
| Log nhiều pod | ❌ | ✅ | ✅ | ◐ | ❌ | ◐ |
| `can-i` trước khi bấm | ✅ | ❌ | ❌ | ◐ | ✅ | ✅ |
| Audit log | ✅ | ❌ | ❌ | ❌ | trả phí | ✅ |
| Đánh dấu production | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Gộp SSH/SFTP/Docker/VPS | ✅ | ❌ | ❌ | ❌ | ◐ | ❌ |

## Tiện dụng theo user (1–5)

| User | Điểm | Thiếu chính |
|---|---|---|
| Sysadmin (VPS, k3s/MicroK8s) | 5 | Gần như không |
| Dev | 4 | Log nhiều pod, sửa YAML, CRD |
| Người mới học K8s | 4 | Giải thích trạng thái lỗi |
| DevOps / SRE | 3 | apply/Helm, metrics lịch sử, so sánh nhiều cluster |
| Platform / cluster admin | 3 | RBAC, CRD, quota |
| Security / audit | 3 | Đẩy log ra ngoài, xem RBAC |
| Quản lý (chỉ xem) | 2 | Chế độ chỉ đọc theo vai trò |

## Khoảng trống, xếp theo ưu tiên

| # | Việc | Ai được lợi | Công sức | Ghi chú |
|---|---|---|---|---|
| 1 | Xem resource tuỳ ý và CRD (chỉ đọc) | Mọi người | Vừa | Thiếu lớn nhất; cluster thật hầu như đều có CRD |
| 2 | Log nhiều pod theo selector/deployment | Dev, SRE | Vừa | Có sẵn màn Logs để mở rộng |
| 3 | Gợi ý khi lỗi (CrashLoop, ImagePull, Pending) | Mới học, sysadmin | Thấp | Chỉ đọc event và status đã có |
| 4 | Port-forward cho cluster dùng kubeconfig | Dev | Vừa | Nút đang báo chưa làm được; dùng `portforward` WebSocket |
| 5 | Metrics có lịch sử (sparkline trong phiên, hoặc đọc Prometheus) | SRE, sysadmin | Vừa | Tuỳ chọn |
| 6 | Sửa YAML an toàn (diff, hỏi xác nhận, audit) | DevOps | Cao | Phá nguyên tắc "không apply", cần quyết định |

## Cần quyết định

1. Giữ nguyên tắc "không `apply`/`edit`", hay cho sửa YAML của resource có sẵn (không tạo mới)?
2. Helm: bỏ hẳn, hay chỉ xem danh sách release (chỉ đọc)?
3. Hướng chính: sysadmin/dev (làm 1–4, giữ gọn) hay DevOps/SRE (thêm 5–6, đối đầu Lens)?

Đề xuất: làm 3, 1, 2, 4 trước (dev và sysadmin, rủi ro thấp, giữ nguyên tắc chỉ đọc), rồi mới bàn 6.
