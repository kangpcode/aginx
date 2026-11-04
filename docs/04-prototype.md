# Tugas 4 — POC siap kompilasi

Kode lengkap berada pada source tree dan disalin ke appendix dokumen ini dengan nama file di atas setiap code block. Target Linux; semua komentar implementasi berbahasa Indonesia. Tidak ada dependency pihak ketiga. `go.mod` menyatakan Go 1.24; gunakan patch terbaru dari cabang yang masih didukung untuk penggunaan di luar eksperimen.

```bash
make build
make test
make vet
make race
make fuzz
./bin/aginx -t -config examples/www/.ginxaccess
./bin/aginx -listen 127.0.0.1:8080 -root examples/www
```

Build mengaktifkan `CGO_ENABLED=0`; `-buildvcs=false` memungkinkan source archive tanpa metadata Git. Verifikasi static linkage:

```bash
file bin/aginx
readelf -l bin/aginx
readelf -d bin/aginx
```

Tidak boleh ada program header `INTERP` atau dynamic dependency `NEEDED`. Binary race test menggunakan CGO; binary distribusi tetap dibangun terpisah tanpa CGO.

```bash
curl -i http://127.0.0.1:8080/
curl -I http://127.0.0.1:8080/index.html
curl -i http://127.0.0.1:8080/welcome
curl --path-as-is -i http://127.0.0.1:8080/%2e%2e/etc/passwd
wrk -t4 -c128 -d30s --latency http://127.0.0.1:8080/index.html
```

Demonstrasi perubahan tanpa restart pada shell kedua; ini mengubah file contoh di working copy:

```bash
cat > examples/www/.ginxaccess.next <<'EOF'
header set X-Powered-By "AGinX-updated";
rewrite "^/welcome$" "/index.html" last;
EOF
./bin/aginx -t -config examples/www/.ginxaccess.next
mv examples/www/.ginxaccess.next examples/www/.ginxaccess
sleep 0.2
curl -I http://127.0.0.1:8080/index.html
```

TTL divalidasi saat request masuk, bukan background timer. Batas staleness adalah interval TTL ditambah waktu I/O/scheduling untuk request yang menemukan entry expired. SIGHUP hanya memaksa metadata revalidation, tidak memuat global config/listener baru. SIGTERM/SIGINT menutup listener dan idle connection lalu drain response aktif; sesudah 10 s socket sisanya ditutup. Kernel I/O yang macet tetap dapat menahan goroutine, sehingga production membutuhkan pembatasan disk I/O dan supervisor dengan deadline proses.

Untuk sinyal, ambil PID ketika memulai server dari shell yang sama:

```bash
./bin/aginx -root examples/www &
aginx_pid=$!
kill -HUP "$aginx_pid"
kill -TERM "$aginx_pid"
wait "$aginx_pid"
```

Implementasi parser menerima HTTP/1.1 dan 1.0 origin-form. HTTP/1.1 memerlukan satu Host. CRLF wajib, obs-fold dan whitespace sebelum colon ditolak. Duplicate Content-Length, CL+TE, setiap Transfer-Encoding, dan request body nonkosong ditolak lalu koneksi ditutup. Body/chunked dapat ditambahkan dengan state machine framing sebelum proxy/FastCGI, bukan dengan mengabaikan byte sisa. POC menulis response Content-Length, mengimplementasikan HEAD tanpa body, mengurutkan response pipelining, dan menutup koneksi setelah maksimal 1000 request. [Aturan framing HTTP/1.1](https://www.rfc-editor.org/rfc/rfc9112.html).

Header deadline default 5 s, idle 30 s, write 30 s; max header termasuk request line 16 KiB dan 100 fields, koneksi default 10K. Byte pertama pada keep-alive memakai idle timeout lalu header memakai absolute deadline. Partial write header/body ditangani loop; file memakai `io.CopyN` dengan descriptor terpisah per request. Runtime netpoll/sendfile menangani EAGAIN, EINTR dan partial transfer tanpa busy-loop. [Implementasi Go](https://go.dev/src/internal/poll/sendfile_unix.go).

Path diperiksa sesudah URL decode, sebelum canonicalize, dengan penolakan dot components/hidden files, backslash, NUL, control dan kedalaman berlebih. Prefix docroot diperiksa lagi, lalu `openat` berantai dengan `O_NOFOLLOW` pada setiap komponen menjaga symlink tidak dapat dialihkan setelah pemeriksaan string. FIFO dibuka dengan O_NONBLOCK lalu ditolak karena bukan regular file; file special lain juga ditolak. Docroot wajib dikelola publisher terpercaya: tidak ada jaminan terhadap admin yang memasang bind mount, hardlink rahasia, atau menulis isi file langsung saat sedang dikirim. Atomic rename memberi snapshot descriptor file yang konsisten untuk pembaruan content.

`-access=false` hanya digunakan untuk benchmark baseline pada docroot tanpa policy. Menonaktifkannya pada docroot yang membutuhkan ACL akan mematikan pemeriksaan ACL. Jangan menganggap full language di `examples/spec` dapat dijalankan oleh parser POC.

POC belum mencakup master-worker, SO_REUSEPORT, inotify, TLS, HTTP/2/3, auth, Range/ETag/304, chunked request, gzip/Brotli, configurable index, maupun FastCGI. Directory listing selalu off; index fixed `index.html`. Header config diterapkan pada response static sukses; integrasi header pada semua error response adalah pekerjaan engine lanjutan. Error config pertama menghasilkan 503, tidak crash. Paket ini belum merupakan release production.

## Appendix kode lengkap

Bagian berikut dihasilkan dari file implementasi dengan `python3 scripts/render-source.py`.
