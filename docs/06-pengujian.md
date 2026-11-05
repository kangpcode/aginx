# Tugas 6 — Pengujian dan benchmark

Target pengujian dipisahkan dari hasil aktual di [VERIFIKASI.md](VERIFIKASI.md). Belum ada hasil RPS/p99, 100K koneksi atau soak 24 jam. Jangan menerjemahkan microbenchmark cache menjadi klaim overhead HTTP end-to-end.

## Jenis pengujian dan kriteria lulus

| Jenis | Cakupan | Kriteria |
|---|---|---|
| Unit | Lexer, parser, ACL IPv4/IPv6, rewrite capture, header validation, size/depth/rule limits | Deterministik, error berisi baris, input invalid tidak diterima diam-diam |
| Integration | Byte stream HTTP, keep-alive, pipelining, HEAD, file besar, update config concurrent, signal/drain | Body checksum cocok, framing tidak desync, ACL tidak terlewati |
| Security | Traversal encoded/raw, hidden files, symlink alias, FIFO, CL/TE, slowloris, XFF spoofing | Tidak membocorkan file/source, resource bounded, koneksi invalid ditutup |
| Fuzz | HTTP parser, config parser; selanjutnya AST evaluator/header serialization | Tidak panic/hang; waktu/memory dibatasi; simpan corpus regresi |
| Race | Config publish, LRU eviction, invalidation, connection lifecycle, shutdown | `go test -race` bersih pada workload uji |
| Load | Static 1 KiB/64 KiB/1 MiB, keep-alive/non-keep-alive, depth 0/8/32 | Error dan timeout tercatat; RPS dan p99 dibanding baseline yang setara |
| Soak | 24 jam dengan perubahan config atomic dan traffic stabil/burst | Tidak ada slope FD/goroutine/RSS tak terbatas; tidak ada crash/ACL bypass |
| Chaos | FD exhaustion, disk lambat, quota/full, SIGTERM/HUP, worker crash; watcher overflow saat tersedia | Backpressure/fail-closed/last-good sesuai kontrak; rollback terdokumentasi |

Perintah verifikasi:

```bash
make check
make fuzz
make bench
AGINX_REQUIRE_TCP=1 make test
GOCACHE=/tmp/aginx-go-cache go test ./... -cover
GOCACHE=/tmp/aginx-go-cache go test ./internal/server -v
```

Integration suite memakai TCP loopback bila tersedia. Bila sandbox menolak pembuatan socket dengan EPERM/EACCES, suite memakai `net.Pipe` dan menulis nama transport ke verbose log. Ini menguji byte stream, timeout, dan lifecycle tanpa membuktikan perilaku TCP/kernel sendfile. `AGINX_REQUIRE_TCP=1` membuat keterbatasan tersebut menjadi kegagalan dan digunakan CI. Race detector menemukan race yang terlewati oleh workload uji; kelulusan bukan pembuktian bebas semua race.

Fuzz smoke 10 detik per target hanya pemeriksaan awal. Jalankan 1–24 jam di host khusus, batasi worker/memory dan simpan corpus yang memperluas coverage. Tambahkan differential HTTP corpus dari RFC, mutation CL/TE/whitespace, abrupt disconnect, dan state-machine transitions setelah body/proxy tersedia.

## Rancangan eksperimen

Catat CPU/core/SMT, RAM, NIC/link, kernel, distro, filesystem/mount, governor, Go build info, Nginx/Apache version, config lengkap, dan checksum file. Gunakan load generator terpisah untuk menghindari bottleneck client dan konflik CPU. Pin server/client pada CPU berbeda bila loopback dipakai, catat batas cgroup. Warm-up 10–30 s, ukur minimal lima run masing-masing 30–60 s, acak urutan A/B, laporkan median serta variasi. Jangan flush page cache host bersama; cold-cache test hanya di mesin/VM khusus.

Mode A satu core: `GOMAXPROCS=1` untuk satu proses AGinX dan satu worker Nginx. Mode B multi-core: satu Go process memakai jumlah core yang dicatat versus jumlah worker Nginx setara; mode worker-per-core AGinX belum tersedia. Samakan keep-alive, connection cap, asset, access logging, compression dan TLS. TLS benchmark dilaporkan terpisah karena plaintext sendfile tidak mewakili TLS userspace.

Jalankan baseline AGinX tanpa policy hanya pada docroot benchmark khusus. `-access=false` menonaktifkan ACL. Contoh tiga eksperimen di terminal terpisah, satu server AGinX aktif pada satu waktu:

```bash
GOMAXPROCS=1 ./bin/aginx -listen 127.0.0.1:8080 -root examples/www -access=false
bash bench/run.sh http://127.0.0.1:8080/index.html
```

Simpan hasil baseline dengan directory terpisah:

```bash
AGINX_BENCH_RESULTS=bench/results/baseline bash bench/run.sh http://127.0.0.1:8080/index.html
```

Hentikan baseline, mulai policy mode, lalu ukur:

```bash
GOMAXPROCS=1 ./bin/aginx -listen 127.0.0.1:8080 -root examples/www -access=true
AGINX_BENCH_RESULTS=bench/results/policy bash bench/run.sh http://127.0.0.1:8080/index.html
```

Nginx baseline contoh memakai port 8081:

```bash
nginx -p "$PWD/" -c bench/nginx.conf
AGINX_BENCH_RESULTS=bench/results/nginx bash bench/run.sh http://127.0.0.1:8081/index.html
```

Policy mode contoh mempunyai header/rewrite sehingga overhead mencakup evaluasi rule dan bytes response tambahan. Untuk mengisolasi biaya discovery cache-hit, gunakan docroot salinan identik dengan `.ginxaccess` kosong pada depth 0/8/32, lalu ulangi suite dengan real policy. TTL revalidation, rule evaluation, dan watcher invalidation dilaporkan terpisah dari cache-hit murni.

Overhead throughput = `100 * (RPS_baseline - RPS_policy) / RPS_baseline`. Target <5% dinilai pada median run dan interval variasi yang lebih kecil dari 5%, dengan error rate dan p99 tidak memburuk secara material. Jika variabilitas lebih besar, eksperimen belum dapat menyimpulkan target tercapai. Cache LRU POC mempunyai lock bersama dan traversal per-parent; kemungkinan perlu sharding/premerged directory snapshots sebelum target tercapai.

Tetapkan gate kompetitif static sebagai hipotesis awal: median RPS AGinX >=90% Nginx dan p99 <=1,20x Nginx pada matrix workload yang disepakati, error <0,1%. Ini definisi penerimaan proyek, bukan sifat bahasa atau hasil saat ini. Untuk overhead policy gunakan baseline AGinX yang sama, bukan Nginx. Untuk memory gunakan slope `(RSS_N - RSS_0) / N`, sesudah warm-up dan stabilisasi, lalu laporkan juga `ss -m`/cgroup socket memory karena RSS tidak memuat seluruh biaya kernel.

## Perintah benchmark

```bash
wrk -t4 -c128 -d30s --latency http://127.0.0.1:8080/index.html
ab -k -n100000 -c128 http://127.0.0.1:8080/index.html
h2load --h1 -n100000 -c128 -t4 http://127.0.0.1:8080/index.html
```

`wrk --latency` memberi distribusi latency untuk request yang berhasil; laporkan socket error/timeout dan coordinated-omission risk. `ab` berguna sebagai pembanding sederhana, bukan satu-satunya sumber p99. h2load memakai `--h1` untuk POC; HTTP/2 benchmark baru valid setelah HTTP/2 diimplementasikan. [wrk](https://github.com/wg/wrk), [ApacheBench](https://httpd.apache.org/docs/2.4/programs/ab.html), [h2load](https://nghttp2.org/documentation/h2load.1.html).

Setelah HTTP/2/TLS tersedia, gunakan h2load tanpa `--h1`, dengan stream concurrency yang ditetapkan eksplisit:

```bash
h2load -n100000 -c128 -m100 -t4 https://server-benchmark.example/index.html
```

Contoh soak plan untuk host khusus, belum dijalankan:

```bash
wrk -t8 -c4096 -d24h --latency http://server-benchmark.example/index.html
```

Load 100K koneksi memerlukan dedicated idle-client harness yang membuka koneksi bertahap, mengirim satu request, kemudian menjaga idle tanpa melampaui timeout. Jangan menyebut `wrk -c100000` sebagai pengukuran idle memory karena ia terus mengirim request aktif. Uji koneksi idle, active slow-reader, churn, dan mixed activity secara terpisah. Default cap 10K dinaikkan hanya sesudah perhitungan budget FD/RAM dan pengukuran bertahap.

Metrik wajib: RPS/error/timeout, p50/p95/p99, bytes/s, user/system CPU, RSS/PSS, FD, goroutine/thread, socket memory, GC assist/pause, config hit/miss/stale/parse-error, invalidation lag, serta queue depth. POC belum mengekspos metrics endpoint; gunakan tool OS untuk bagian yang tersedia dan tambah instrumentation bounded cardinality pada fase berikutnya.

```bash
# Isi PID dari proses benchmark yang dimulai sendiri.
ps -p "$aginx_pid" -o pid,pcpu,rss,nlwp,etime
ls "/proc/$aginx_pid/fd" | wc -l
cat "/proc/$aginx_pid/status"
cat "/proc/$aginx_pid/smaps_rollup"
ss -s
pidstat -p "$aginx_pid" -r -u -w 1
```

Untuk membuktikan fast path pada host yang mengizinkan socket dan ptrace, jalankan pada terminal pertama:

```bash
strace -f -e trace=sendfile -o /tmp/aginx-sendfile.log ./bin/aginx -root examples/www
```

Terminal kedua harus meminta regular file cukup besar; buat fixture pada salinan docroot benchmark, request file tersebut, dan periksa `sendfile(...)` serta jumlah bytes. Bukti source standard library atau keberhasilan transfer `net.Pipe` tidak menggantikan trace syscall. Tracing mengubah performa, jadi jangan menilai RPS saat strace aktif.

## Tuning OS: eksperimen terkontrol

Tidak ada sysctl host yang diubah oleh proyek ini. Rekam nilai awal dan ubah satu parameter setiap eksperimen. Sebagian nilai di bawah adalah titik awal workload besar, bukan default universal.

| Parameter | Titik awal eksperimen dan alasan/trade-off |
|---|---|
| `ulimit -n`, systemd `LimitNOFILE` | 262144 untuk rencana 100K socket dengan headroom file/config; periksa hard limit serta `fs.nr_open`/`fs.file-max` |
| `net.core.somaxconn` | 4096–16384 bila accept queue penuh; backlog lebih besar memakai resource dan tidak menyelesaikan overload aplikasi |
| `net.ipv4.tcp_max_syn_backlog` | 8192 bila SYN queue pressure terukur; dokumentasikan SYN cookies dan retransmit |
| `net.core.netdev_max_backlog` | Sesuaikan hanya jika softnet drops/NIC receive queue menunjukkan bottleneck |
| `net.core.rmem_max/wmem_max`, `tcp_rmem/wmem` | Ukur BDP dan autotuning; jangan membesarkan buffer seluruh 100K socket tanpa RAM budget |
| `net.ipv4.ip_local_port_range` | Sering bottleneck load generator/churn; tambah source IP bila port satu client habis |
| `net.ipv4.tcp_tw_reuse` | Pertahankan default sistem kecuali eksperimen koneksi outgoing membutuhkannya; bukan solusi generik koneksi inbound server |
| `tcp_fin_timeout` | Jangan dipakai sebagai cara memangkas semua TIME_WAIT; semantiknya berbeda |
| THP | Bandingkan mode default/madvise/never pada dedicated host; pantau allocation/compaction latency dan RSS, jangan menonaktifkan global tanpa bukti |
| CPU governor, IRQ/RPS/XPS/NUMA | Catat dan kontrol saat benchmark; affinity dapat mengurangi migrasi tetapi memperburuk load imbalance |
| inotify watch/instance limit | Diperlukan nanti; subtree watch bounded dan fallback TTL tetap wajib |

Kernel mendokumentasikan `tcp_tw_reuse` sebagai pengaturan yang sebaiknya tidak diubah tanpa alasan ahli; default dapat berbeda pada distro/kernel. [IP sysctl](https://docs.kernel.org/networking/ip-sysctl.html). THP dapat mengurangi TLB miss, sekaligus mempunyai biaya memory/compaction yang bergantung workload. [THP documentation](https://docs.kernel.org/admin-guide/mm/transhuge.html).

Rencana chaos dijalankan pada VM/container khusus, tanpa mengubah resource host bersama: batasi FD dengan `prlimit`, memory lewat cgroup, disk quota/full, rename/delete `.ginxaccess` berulang, inject syntax error dan pulihkan, SIGTERM saat slow-reader aktif, lalu restart generasi baru. Ketika inotify/master-worker tersedia, tambah overflow queue, worker crash, binary readiness failure, dan rollback. Lulus jika tidak ada kebocoran konten privat, last-good/fail-closed sesuai kontrak, serta resource kembali stabil setelah gangguan berakhir.

Keputusan tetap Go/netpoll dengan static serving terkonfinen dan snapshot policy. Risiko utama yang belum terselesaikan adalah parser full HTTP, memory/GC 100K koneksi, cold disk, inotify dan binary upgrade. Next action: jalankan TCP suite dan benchmark A/B pada host yang sesuai, kemudian gunakan hasil profiling untuk memilih optimasi pertama.
