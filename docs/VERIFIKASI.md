# Verifikasi lokal — 4 November 2026

Environment: Linux 7.0.0-42-generic x86_64, Go 1.27.0, CPU AMD Ryzen 5 6600H. Workspace menyediakan akses tulis source dan `/tmp`, tetapi menolak pembuatan socket TCP dengan `operation not permitted`. Tidak ada service yang dipasang atau sysctl host yang diubah.

| Pemeriksaan | Hasil aktual |
|---|---|
| `make check` | PASS: `go vet`, unit/integration suite, `go test -race`, static build |
| HTTP fuzz smoke terakhir | PASS, 10 detik, 2 worker, 567.488 eksekusi |
| Config fuzz smoke terakhir | PASS, sekitar 11 detik termasuk penyelesaian worker, 8.050 eksekusi |
| CLI `-t` pada contoh POC | PASS |
| CLI `-t` pada config invalid/missing | PASS: exit nonzero; syntax error menyertakan baris |
| `file` + `readelf -l/-d` | ELF x86-64 static, stripped, tanpa INTERP/dynamic section |
| Ukuran binary | 5.144.736 byte pada build lokal ini |
| Protocol integration transport | `net.Pipe`; TCP sungguhan belum tervalidasi lokal |
| Cache-hit microbenchmark | Satu run paralel: 119,5 ns/op, 0 B/op, 0 allocs/op; bukan ukuran overhead HTTP |
| Concurrent config update regression | PASS pada 30 pengulangan suite yang diinstrumentasi coverage |
| Shell syntax benchmark script | PASS `bash -n bench/run.sh` |
| Appendix source | Dihasilkan ulang dari file implementasi lengkap |

Perintah yang dijalankan:

```bash
make check
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
```

Coverage statement per paket pada run terakhir: http1 92,3%, ginxaccess 86,0%, server 90,5%, static 82,9%. CLI dan fsroot belum mempunyai direct package test; fsroot dipanggil integration/static/cache tests, tetapi instrumentasi `go test -cover` biasa tidak menghitung lintas paket. Angka coverage tidak membuktikan bebas bug atau kesiapan production.

Kasus yang lulus mencakup framing CL/TE/duplicate Host, header besar dan slow header, keep-alive/pipelining/HEAD, checksum transfer file besar, partial write, drain response aktif, shutdown deadline, batas koneksi/idle, traversal encoded/raw, hidden file, symlink keluar/alias internal, FIFO, penolakan script server-side dasar, rewrite cycle/capture expansion limit, ACL parent/child, ACL sumber/tujuan rewrite, XFF spoofing, first-invalid fail closed, last-known-good setelah syntax error/SIGHUP, negative cache/create/delete/rename, bounded cache, serta concurrent config publish.

Saat pengujian coverage, concurrent atomic rename pernah menghasilkan 503: unlink inode lama mengubah ctime descriptor meskipun isi snapshot tidak berubah. Perbaikannya mempertahankan ctime pada identity cache, tetapi validasi konsistensi pembacaan memakai device/inode/mtime/size. Pemeriksaan akhir dan regression berulang lulus setelah perbaikan.

Yang belum dibuktikan: syscall sendfile aktual pada TCP, perilaku TCP saat congestion/disconnect, signal pada daemon live, benchmark wrk/h2load/ab, pembandingan Nginx/Apache, memory idle 100K koneksi, overhead policy <5%, soak 24 jam, chaos load, CI GitHub, packaging .deb/.rpm, compatibility kernel 4.18 dan empat distro. Tool benchmark/Nginx tidak tersedia pada environment ini. CI dikonfigurasi memakai `AGINX_REQUIRE_TCP=1` agar transport fallback tidak menyamarkan kekurangan verifikasi TCP.

Langkah berikutnya pada host yang mendukung socket:

```bash
AGINX_REQUIRE_TCP=1 make check
./bin/aginx -root examples/www -listen 127.0.0.1:8080
# Terminal lain:
curl -i http://127.0.0.1:8080/welcome
bash bench/run.sh http://127.0.0.1:8080/index.html
```

Desain yang sudah diputuskan adalah Go/netpoll, static binary, confinement descriptor, dan snapshot `.ginxaccess` dengan TTL. Risiko berikutnya adalah memory/GC pada skala besar, blocking disk I/O, full HTTP framing, inotify consistency, dan master-worker/binary upgrade. Gunakan hasil TCP/benchmark untuk menentukan pekerjaan optimasi berikutnya.
