# Verifikasi lokal — 5 November 2026

Environment: Linux 7.0.0-42-generic x86_64, Go 1.27.0, CPU AMD Ryzen 5 6600H. Sandbox menolak pembuatan socket TCP dengan `operation not permitted`; suite TCP wajib dan smoke binary berhasil dijalankan di luar sandbox dengan izin eksekusi. Tidak ada service yang dipasang atau sysctl host yang diubah.

| Pemeriksaan | Hasil aktual |
|---|---|
| `make check` dalam sandbox | PASS: `go vet`, unit/integration suite melalui net.Pipe, `go test -race`, static build |
| `AGINX_REQUIRE_TCP=1 make check` di luar sandbox | PASS: integration TCP loopback, race detector, vet, static build |
| HTTP fuzz smoke terakhir | PASS, 10 detik, 2 worker, 568.854 eksekusi |
| Config fuzz smoke terakhir | PASS, sekitar 11 detik termasuk penyelesaian worker, 23.791 eksekusi |
| CLI `-t` pada contoh POC | PASS |
| CLI `-t` pada config invalid/missing | PASS: exit nonzero; syntax error menyertakan baris |
| `file` + `readelf -l/-d` | ELF x86-64 static, stripped, tanpa INTERP/dynamic section |
| Ukuran binary | 5.144.736 byte pada build lokal ini |
| Protocol integration transport | TCP loopback pada suite wajib; net.Pipe juga lulus dalam sandbox |
| Smoke binary live | PASS: tujuh kelompok pemeriksaan CLI, protocol/policy, update, sinyal, dan transfer 10 MiB |
| Syscall sendfile | Teramati pada file-to-TCP, termasuk partial transfer/EAGAIN dan penyelesaian setelah SIGTERM |
| Cache-hit microbenchmark | Satu run paralel: 163,7 ns/op, 0 B/op, 0 allocs/op; host berbagi beban fuzz, bukan ukuran overhead HTTP |
| Concurrent config update regression | PASS pada suite TCP/race terbaru; 30 pengulangan coverage juga lulus pada verifikasi 4 November |
| Shell syntax benchmark script | PASS `bash -n bench/run.sh` |
| Appendix source | Dihasilkan ulang dari file implementasi lengkap |

Perintah yang dijalankan:

```bash
make check
AGINX_REQUIRE_TCP=1 make check
make fuzz
make bench
GOCACHE=/tmp/aginx-go-cache go test ./... -cover
GOCACHE=/tmp/aginx-go-cache go test ./internal/server -v -count=1
GOCACHE=/tmp/aginx-go-cache go test ./internal/server -run TestConcurrentClientsAndConfigChange -cover -count=30
./bin/aginx -t -config examples/www/.ginxaccess
file bin/aginx
readelf -l bin/aginx
readelf -d bin/aginx
bash -n bench/run.sh
python3 scripts/render-source.py
strace -f -e trace=sendfile -o /tmp/aginx-sendfile-smoke.log python3 scripts/live-smoke.py
rg 'sendfile' /tmp/aginx-sendfile-smoke.log
```

Coverage statement per paket pada run terakhir: http1 92,3%, ginxaccess 86,0%, server 90,5%, static 82,9%. CLI dan fsroot belum mempunyai direct package test; fsroot dipanggil integration/static/cache tests, tetapi instrumentasi `go test -cover` biasa tidak menghitung lintas paket. Angka coverage tidak membuktikan bebas bug atau kesiapan production.

Smoke binary menggunakan docroot sementara dan port loopback ephemeral. Tujuh kelompok yang lulus: CLI config valid/invalid/missing; GET/HEAD pada socket keep-alive yang sama beserta rewrite/header; ACL sumber/tujuan dan traversal; update config atomic tanpa reload/restart; last-known-good setelah syntax error dan SIGHUP lalu recovery; penolakan CL+TE dengan socket close; serta SIGTERM saat response file 10.485.760 byte aktif, dengan checksum SHA-256 yang tetap cocok dan exit daemon 0.

Cuplikan trace aktual, dengan baris lain dihilangkan:

```text
22869 sendfile(7, 8, NULL, 10485760 <unfinished ...>
22863 --- SIGTERM {si_signo=SIGTERM, si_code=SI_USER, si_pid=22847, si_uid=1000} ---
22869 <... sendfile resumed>)           = 2636800
22869 sendfile(7, 8, NULL, 7848960)     = 95232
22869 sendfile(7, 8, NULL, 7753728)     = -1 EAGAIN (Resource temporarily unavailable)
22869 sendfile(7, 8, NULL, 7753728)     = 1000112
22869 sendfile(7, 8, NULL, 336282)      = 336282
22863 +++ exited with 0 +++
```

Trace lengkap run ini berada di `/tmp/aginx-sendfile-smoke.log`; jalankan ulang perintah di atas untuk mereproduksi. Besar partial transfer dan jumlah EAGAIN dapat berbeda. Tracing memengaruhi timing, sehingga run ini bukan pengukuran throughput atau p99.

Kasus yang lulus mencakup framing CL/TE/duplicate Host, header besar dan slow header, keep-alive/pipelining/HEAD, checksum transfer file besar, partial write, drain response aktif, shutdown deadline, batas koneksi/idle, traversal encoded/raw, hidden file, symlink keluar/alias internal, FIFO, penolakan script server-side dasar, rewrite cycle/capture expansion limit, ACL parent/child, ACL sumber/tujuan rewrite, XFF spoofing, first-invalid fail closed, last-known-good setelah syntax error/SIGHUP, negative cache/create/delete/rename, bounded cache, serta concurrent config publish.

Saat pengujian coverage, concurrent atomic rename pernah menghasilkan 503: unlink inode lama mengubah ctime descriptor meskipun isi snapshot tidak berubah. Perbaikannya mempertahankan ctime pada identity cache, tetapi validasi konsistensi pembacaan memakai device/inode/mtime/size. Pemeriksaan akhir dan regression berulang lulus setelah perbaikan.

Yang belum dibuktikan: perilaku TCP saat congestion/disconnect di jaringan nyata, benchmark wrk/h2load/ab, pembandingan Nginx/Apache, memory idle 100K koneksi, overhead policy <5%, soak 24 jam, chaos load, CI GitHub, packaging .deb/.rpm, compatibility kernel 4.18 dan empat distro. Tool benchmark/Nginx tidak tersedia pada environment ini. CI dikonfigurasi memakai `AGINX_REQUIRE_TCP=1` agar transport fallback tidak menyamarkan kekurangan verifikasi TCP, serta menjalankan smoke binary.

Langkah berikutnya pada host yang mendukung socket:

```bash
AGINX_REQUIRE_TCP=1 make check
make smoke
./bin/aginx -root examples/www -listen 127.0.0.1:8080
# Terminal lain:
curl -i http://127.0.0.1:8080/welcome
bash bench/run.sh http://127.0.0.1:8080/index.html
```

Desain yang sudah diputuskan adalah Go/netpoll, static binary, confinement descriptor, dan snapshot `.ginxaccess` dengan TTL. Risiko berikutnya adalah memory/GC pada skala besar, blocking disk I/O, full HTTP framing, inotify consistency, dan master-worker/binary upgrade. Gunakan hasil TCP/benchmark untuk menentukan pekerjaan optimasi berikutnya.
