# Kubernetes trong Panel: so sánh đối thủ và hướng đi

Ngày: 2026-11-05. Cột Vutty đã đối chiếu với code và với một cluster kind thật (cả hai đường vào). Cột đối thủ lấy từ kiến thức chung, **chưa kiểm lại phiên bản mới nhất**; ô nào chưa chắc ghi `?`.

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
| Port-forward | ✅ (host và kubeconfig) | ✅ | ✅ | ✅ | ❌ | ❌ |
| CRD / resource tuỳ ý | ✅ (chỉ đọc) | ✅ | ✅ | ✅ | ◐ | ✅ |
| Sửa YAML / apply | ◐ (chỉ workload, chỉ khi mở bằng kubeconfig; không tạo mới) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Helm | ◐ (chỉ xem release; không có repo/chart) | ✅ | ◐ ? | plugin | ✅ | ✅ |
| Metrics có lịch sử | ❌ (chỉ hiện tại, cần metrics-server) | ✅ | ◐ | plugin | ◐ | ✅ |
| Log nhiều pod | ✅ (tối đa 20 pod) | ✅ | ✅ | ◐ | ❌ | ◐ |
| Giải thích vì sao pod lỗi (CrashLoop, OOM, ImagePull…) | ✅ | ? | ? | ? | ? | ? |
| `can-i` trước khi bấm | ✅ | ❌ | ❌ | ◐ | ✅ | ✅ |
| Audit log | ✅ | ❌ | ❌ | ❌ | trả phí | ✅ |
| Quét bảo mật (pod đặc quyền, hostPath, secret viết thẳng, NodePort, Ingress không TLS) | ✅ (chỉ đọc) | ❌ | ❌ | plugin ? | ◐ | ✅ (trả phí ?) |
| Container debug tạm (`kubectl debug`) cho pod không có shell | ✅ (cả hai đường vào) | ◐ ? | ◐ ? | ❌ ? | ❌ | ❌ ? |
| Copy lệnh kubectl tương đương | ✅ | ◐ | ❌ | ❌ | ❌ | ◐ |
| File của pod, không cài gì vào pod | ✅ (chỉ đọc) | ❌ | ❌ | ❌ | ❌ | ❌ |
| Đánh dấu production | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Gộp SSH/SFTP/Docker/VPS | ✅ | ❌ | ❌ | ❌ | ◐ | ❌ |

Điều chỉnh so với bản trước: Metrics của Vutty hạ từ ◐ xuống ❌ (không có lịch sử, chỉ số hiện tại); ô Sửa YAML ghi rõ chỉ chạy với kubeconfig; thêm dòng "Giải thích vì sao pod lỗi".

## Tiện dụng theo user (1–5)

Điểm giữ nguyên, nhưng phần còn thiếu đã nhỏ lại.

| User | Điểm | Thiếu chính |
|---|---|---|
| Sysadmin (VPS, k3s/MicroK8s) | 5 | Gần như không |
| Dev | 5 | Sửa YAML ngoài workload và CRD. (Log nhiều pod và xem CRD đã có) |
| Người mới học K8s | 4 | Đã có lý do cho CrashLoop, OOM, ImagePull, Evicted, Init, Pending; thiếu hướng dẫn cách sửa và các lỗi khác (PVC, probe, quota: chưa kiểm) |
| DevOps / SRE | 3 | apply, thay đổi release Helm (hiện chỉ xem), metrics lịch sử, so sánh nhiều cluster |
| Platform / cluster admin | 3 | RBAC, quota. (CRD xem được, chỉ đọc) |
| Security / audit | 3 | Đẩy log ra ngoài, xem RBAC |
| Quản lý (chỉ xem) | 2 | Chế độ chỉ đọc theo vai trò (hiện chỉ có khoá nút theo `can-i`) |

## Khoảng trống, xếp theo ưu tiên

| # | Việc | Ai được lợi | Công sức | Ghi chú |
|---|---|---|---|---|
| 1 | ~~Xem resource tuỳ ý và CRD (chỉ đọc)~~ **đã làm** (tab Resources) | Mọi người | Vừa | |
| 2 | ~~Log nhiều pod theo selector/deployment~~ **đã làm** (Logs trên workload và Service) | Dev, SRE | Vừa | |
| 3 | Gợi ý khi lỗi: **làm một phần** (cột Why). Còn: cách sửa, các lỗi khác | Mới học, sysadmin | Thấp | Chỉ đọc event và status đã có |
| 4 | ~~Port-forward cho cluster dùng kubeconfig~~ **đã làm** (qua API server, đã thử trên kind 1.37) | Dev | Vừa | |
| 5 | Metrics có lịch sử (sparkline trong phiên, hoặc đọc Prometheus) | SRE, sysadmin | Vừa | Tuỳ chọn |
| 6 | Sửa YAML ngoài workload và cho CRD | DevOps | Cao | Workload đã sửa được (Edit YAML: dry-run, diff, audit, chặn khi đối tượng đã đổi) |
| 7 | Sửa workload cho cluster mở bằng host (kubectl) | Sysadmin, DevOps | Vừa | Hiện báo "cần kubeconfig"; đường host chưa có |

## Cần quyết định

1. Mở rộng sửa YAML sang resource khác và CRD không (mục 6), và làm cho đường host (mục 7)?
2. Helm: đã chọn chỉ xem release (tab Apps). Có cần ghi thay đổi (rollback, upgrade) không? Hiện dùng terminal; nút Copy rollback có sẵn.
3. Hướng chính: sysadmin/dev (làm 3, 4, 7, giữ gọn) hay DevOps/SRE (thêm 5, 6, đối đầu Lens)?

Ngày 2026-11-05 (sau #29): thêm tab **Security**, nút **Copy kubectl**, **Debug** (container tạm) và port-forward cho cluster mở bằng kubeconfig ([KubernetesPanel.md](KubernetesPanel.md) mục 25); dòng File của pod bổ sung vào bảng. Ô đối thủ mới ghi `?` là chưa kiểm.

Tiến độ: mục 1 và 2 đã xong; mục 3 làm một phần; mục 4 đã xong; mục 5, 7 chưa làm; mục 6 chờ quyết định.

## Đã làm sau khi so với sidebar của Rancher

Tab **Resources** (mọi loại resource và CRD, chỉ đọc, kèm nút Namespaces, HPA, PersistentVolumes, StorageClasses), tab **Apps** (release Helm, chỉ đọc), log của tất cả pod của Deployment, StatefulSet, DaemonSet, Job và Service, và số đếm trên tab Pods, Workloads, Network. Chi tiết ở [KubernetesPanel.md](KubernetesPanel.md) (mục 20–23).

Chưa làm và lý do: Charts và Repositories của Rancher nằm ngoài cluster (cluster quản lý của Rancher, không phải cluster đang xem); Tools là của riêng Rancher; số đếm cho Events và Config bị bỏ vì tab hiển thị khác với số loại đếm được.
