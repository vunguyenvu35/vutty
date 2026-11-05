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
| CRD / resource tuỳ ý | ✅ (chỉ đọc) | ✅ | ✅ | ✅ | ◐ | ✅ |
| Sửa YAML / apply | ◐ (sửa workload, không tạo mới) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Helm | ◐ (chỉ xem release) | ✅ | ❌ | plugin | ✅ | ✅ |
| Metrics có lịch sử | ◐ | ✅ | ◐ | plugin | ◐ | ✅ |
| Log nhiều pod | ✅ | ✅ | ✅ | ◐ | ❌ | ◐ |
| `can-i` trước khi bấm | ✅ | ❌ | ❌ | ◐ | ✅ | ✅ |
| Audit log | ✅ | ❌ | ❌ | ❌ | trả phí | ✅ |
| Đánh dấu production | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Gộp SSH/SFTP/Docker/VPS | ✅ | ❌ | ❌ | ❌ | ◐ | ❌ |

## Tiện dụng theo user (1–5)

| User | Điểm | Thiếu chính |
|---|---|---|
| Sysadmin (VPS, k3s/MicroK8s) | 5 | Gần như không |
| Dev | 4 | Sửa YAML của resource ngoài workload (CRD hiện chỉ xem); log nhiều pod đã có |
| Người mới học K8s | 4 | Giải thích trạng thái lỗi |
| DevOps / SRE | 3 | apply, thay đổi release Helm (hiện chỉ xem), metrics lịch sử, so sánh nhiều cluster |
| Platform / cluster admin | 3 | RBAC, quota; CRD đã xem được (chỉ đọc) |
| Security / audit | 3 | Đẩy log ra ngoài, xem RBAC |
| Quản lý (chỉ xem) | 2 | Chế độ chỉ đọc theo vai trò |

## Khoảng trống, xếp theo ưu tiên

| # | Việc | Ai được lợi | Công sức | Ghi chú |
|---|---|---|---|---|
| 1 | ~~Xem resource tuỳ ý và CRD (chỉ đọc)~~ **đã làm** (tab Resources) | Mọi người | Vừa | Thiếu lớn nhất; cluster thật hầu như đều có CRD |
| 2 | ~~Log nhiều pod theo selector/deployment~~ **đã làm** (Logs trên workload và Service) | Dev, SRE | Vừa | Có sẵn màn Logs để mở rộng |
| 3 | Gợi ý khi lỗi (CrashLoop, ImagePull, Pending) | Mới học, sysadmin | Thấp | Chỉ đọc event và status đã có |
| 4 | Port-forward cho cluster dùng kubeconfig | Dev | Vừa | Nút đang báo chưa làm được; dùng `portforward` WebSocket |
| 5 | Metrics có lịch sử (sparkline trong phiên, hoặc đọc Prometheus) | SRE, sysadmin | Vừa | Tuỳ chọn |
| 6 | Sửa YAML an toàn (diff, hỏi xác nhận, audit) | DevOps | Cao | Workload đã sửa được (menu Edit YAML); chưa có cho resource khác và CRD |

## Cần quyết định

1. ~~Cho sửa YAML?~~ Workload đã sửa được; còn mở rộng sang resource khác và CRD hay không.
2. ~~Helm~~ Đã chọn chỉ xem release (tab Apps); ghi thay đổi release chưa làm, dùng terminal.
3. Hướng chính: sysadmin/dev (làm 1–4, giữ gọn) hay DevOps/SRE (thêm 5–6, đối đầu Lens)?

Tiến độ: mục 1 và 2 đã xong; mục 3 (gợi ý khi lỗi), 4 (port-forward cho kubeconfig) và 5 (metrics lịch sử) chưa làm; mục 6 chờ quyết định.

## Cập nhật (sau khi so với sidebar của Rancher)

Đã làm: tab **Resources** (mọi loại resource và CRD, chỉ đọc, kèm nút Namespaces, HPA, PersistentVolumes, StorageClasses), tab **Apps** (release Helm, chỉ đọc), log của tất cả pod của Deployment, StatefulSet, DaemonSet, Job và Service, và số đếm trên tab Pods, Workloads, Network. Chi tiết ở [KubernetesPanel.md](KubernetesPanel.md) (mục 20–23).

Chưa làm và lý do: Charts và Repositories của Rancher nằm ngoài cluster (cluster quản lý của Rancher, không phải cluster đang xem); Tools là của riêng Rancher; số đếm cho Events và Config bị bỏ vì tab hiển thị khác với số loại đếm được.
