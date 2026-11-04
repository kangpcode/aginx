Posisikan diri Anda sebagai seorang Principal Software Architect dan Systems Programmer kawakan dengan pengalaman 15+ tahun di bidang High-Performance Networking, Web Server Internals, dan Linux Kernel Networking. Anda menguasai arsitektur Nginx (event-driven, non-blocking, master-worker, zero-copy) hingga level source code, memahami mendalam modularitas dan dynamic per-directory configuration milik Apache HTTPD (.htaccess), serta terbiasa membangun software production-grade yang aman, teruji, dan siap di-deploy.

═══════════════════════════════════════════
1. KONTEKS PROYEK
═══════════════════════════════════════════
Saya (kangpcode / Dhafa Nazula Permadi) membangun web server NATIVE bernama "AGinX" dari nol.
AGinX BUKAN kombinasi/reverse-proxy instalasi Apache + Nginx. AGinX adalah software mandiri (single static binary) yang menggabungkan:

A. Core Engine gaya Nginx:
   - Asynchronous, event-driven, non-blocking I/O multiplexing (epoll di Linux).
   - Arsitektur master-worker, SO_REUSEPORT, worker per-CPU core (CPU affinity opsional).
   - Zero-copy file serving (sendfile/splice), buffer pooling, hemat RAM, target 100K+ koneksi konkuren.

B. Dynamic Configuration Engine gaya Apache:
   - File konfigurasi per-direktori bernama ".ginxaccess" (analog .htaccess).
   - Di-parse & diterapkan secara real-time TANPA reload/restart server.
   - Peningkatan di atas Apache: hasil parse di-cache dan di-invalidate otomatis (inotify/mtime), sehingga TIDAK ada parsing ulang di setiap request.

Target OS: Linux (Debian, Ubuntu, AlmaLinux, CentOS/RHEL-family), kernel >= 4.18.

═══════════════════════════════════════════
2. KRITERIA KEBERHASILAN (WAJIB TERUKUR)
═══════════════════════════════════════════
- Throughput static file: kompetitif dengan Nginx pada hardware yang sama (benchmark: wrk, h2load).
- Overhead pengecekan .ginxaccess pada cache-hit: < 5% dibanding tanpa .ginxaccess.
- Memory per idle connection: serendah mungkin (sebutkan target angka realistis).
- Zero downtime saat file .ginxaccess diubah; graceful shutdown & graceful binary upgrade.
- Lulus: path traversal test, slowloris test, malformed HTTP fuzzing, race detector / sanitizers (ASan, TSan, atau go -race).

═══════════════════════════════════════════
3. TUGAS (KERJAKAN BERURUTAN)
═══════════════════════════════════════════

TUGAS 1 — Rekomendasi Bahasa Pemrograman
- Bandingkan secara objektif: C, Go, dan Rust (sebagai pembanding tambahan).
- Matriks penilaian: performa, latency tail (p99), memory footprint, GC pause, safety/memory-safety, kecepatan development, ekosistem (TLS, HTTP/2, HTTP/3), kemudahan static build, kemudahan packaging .deb/.rpm.
- Berikan KEPUTUSAN FINAL yang konkret (satu bahasa), alasan, risiko, dan mitigasinya.
- Jelaskan bagaimana model I/O dipetakan (epoll langsung vs netpoller Go), serta strategi menghindari blocking call (disk I/O, DNS, file stat) di event loop.

TUGAS 2 — Desain Arsitektur & Struktur Proyek
- Diagram arsitektur (ASCII) alur: accept -> read -> parse -> .ginxaccess interceptor -> routing/handler -> response -> keep-alive/close.
- Pohon direktori lengkap + penjelasan tanggung jawab tiap modul, minimal mencakup:
  core/event-loop, net/listener, http/parser (HTTP/1.1, siap HTTP/2), config/global, config/ginxaccess (lexer, parser, AST, evaluator, cache, watcher), modules (rewrite, acl, headers, auth, cache-control, redirect, error-pages), handlers (static, fastcgi/proxy opsional), tls, logging, metrics, process (master-worker, signal), pool (buffer/memory), tests, bench, packaging (deb/rpm/systemd), docs.
- Jelaskan desain plugin/modul internal (interface/hook phase seperti: post-read, rewrite, access, content, header-filter, log).

TUGAS 3 — Spesifikasi Format ".ginxaccess"
- Rancang sintaksis yang clean, deterministik, mudah di-parse (sertakan grammar EBNF).
- Fitur minimal: URL Rewrite (regex + flag), Redirect (301/302/307/308), Access Control (allow/deny IP & CIDR, order), Custom Headers (set/add/unset), Custom Error Pages, Basic Auth, Cache-Control/Expires per tipe file, Block/Allow User-Agent, Rate limit sederhana, Directory Index, Disable Directory Listing, Hotlink Protection, Conditional blocks (<If>, <Files>, <Match>).
- Aturan pewarisan (inheritance) dari parent directory ke child & aturan override/merge.
- Keamanan: whitelist directive (TIDAK boleh ada eksekusi perintah/kode), batas ukuran file, batas kedalaman include, batas jumlah rule, timeout evaluasi regex (cegah ReDoS), validasi ketat saat parse dengan pesan error berisi nomor baris.
- Strategi cache: key = path + mtime + inode; invalidasi via inotify; fallback stat dengan TTL; perilaku saat file rusak (fail-safe: gunakan versi valid terakhir, catat log error, jangan crash).
- Berikan minimal 5 contoh file .ginxaccess realistis (WordPress-style, API protection, static assets caching, admin area lock, maintenance mode).

TUGAS 4 — Kode Prototype (POC) Siap Kompilasi
- Gunakan bahasa hasil keputusan Tugas 1.
- Wajib mencakup: listener non-blocking, event loop (epoll/netpoller), HTTP/1.1 parser minimal (request line, headers, keep-alive), static file handler dengan sendfile, interceptor .ginxaccess yang menelusuri direktori dari docroot sampai target, cache hasil parse + invalidasi, penerapan minimal: deny/allow IP, set header, rewrite sederhana.
- Perlindungan wajib: path traversal (canonicalize + prefix check), batas header size, timeout koneksi, penanganan EAGAIN/EINTR/partial write, graceful shutdown (SIGTERM/SIGHUP).
- Kode harus lengkap (bukan pseudo-code), memiliki error handling, komentar dalam Bahasa Indonesia, plus Makefile/go.mod dan cara build + cara uji (curl, wrk).

TUGAS 5 — Roadmap & Checklist Tahapan
Sajikan dalam format checklist Markdown ([ ] / [x]) dengan estimasi durasi per fase:

Fase 0 — Fondasi
[ ] Keputusan bahasa & toolchain  [ ] Repo, lisensi, CI (GitHub Actions)  [ ] Coding standard & linter  [ ] Test harness
Fase 1 — Core Engine
[ ] Listener + event loop  [ ] Master-worker & SO_REUSEPORT  [ ] Buffer pool  [ ] Timer (idle/keep-alive/header timeout)  [ ] Graceful shutdown/reload
Fase 2 — HTTP Layer
[ ] HTTP/1.1 parser (strict, anti request-smuggling)  [ ] Keep-alive & pipelining  [ ] Range request  [ ] Chunked encoding  [ ] ETag/Last-Modified/304  [ ] Gzip/Brotli
Fase 3 — Static Serving
[ ] sendfile/zero-copy  [ ] MIME types  [ ] Directory index  [ ] Open-file cache  [ ] Error pages
Fase 4 — .ginxaccess Engine
[ ] Lexer & parser (EBNF)  [ ] AST & evaluator  [ ] Cache + inotify watcher  [ ] Inheritance/merge  [ ] Modul: rewrite, ACL, headers, auth, redirect  [ ] Validator/linter CLI (ginx -t)  [ ] Fuzzing parser
Fase 5 — Keamanan
[ ] TLS 1.2/1.3 + SNI  [ ] Hardening (limits, timeout, slowloris)  [ ] Privilege drop (non-root, chroot/seccomp)  [ ] Security headers default  [ ] Audit & fuzz HTTP parser
Fase 6 — Fitur Lanjutan
[ ] HTTP/2  [ ] Reverse proxy/FastCGI (PHP-FPM)  [ ] Virtual host  [ ] Access/error log + rotasi  [ ] Metrics (Prometheus)  [ ] HTTP/3/QUIC (opsional)
Fase 7 — Packaging & Rilis
[ ] Static binary  [ ] .deb (Debian/Ubuntu)  [ ] .rpm (AlmaLinux/CentOS)  [ ] systemd unit  [ ] Uji di 4 distro target  [ ] Dokumentasi & man page  [ ] Benchmark report vs Nginx/Apache

TUGAS 6 — Strategi Pengujian & Benchmark
- Unit, integration, fuzz, load test, soak test (24 jam), chaos test.
- Perintah benchmark konkret (wrk, h2load, ab) dan metrik yang diukur (RPS, p50/p99, RSS, CPU, FD usage).
- Tuning sistem operasi: ulimit, somaxconn, tcp_tw_reuse, net.core.*, THP, dll.

═══════════════════════════════════════════
4. ATURAN OUTPUT
═══════════════════════════════════════════
- Bahasa: Indonesia teknis; istilah teknis tetap dalam bahasa Inggris.
- Sistematis sesuai nomor Tugas 1-6, tanpa basa-basi, langsung ke arsitektur dan kode.
- Setiap keputusan desain WAJIB disertai alasan dan trade-off.
- Kode dalam code block dengan nama file di atasnya; harus bisa dikompilasi tanpa modifikasi.
- Jika jawaban terlalu panjang, berhenti rapi di akhir satu Tugas dan tulis "LANJUT KE TUGAS X?" agar saya bisa meminta kelanjutan; jangan memotong kode di tengah.
- Jangan berasumsi diam-diam: jika ada informasi yang kurang, ajukan maksimal 3 pertanyaan klarifikasi di awal, lalu tetap lanjutkan dengan asumsi terbaik yang dinyatakan eksplisit.
- Akhiri dengan: (a) ringkasan keputusan arsitektur, (b) daftar risiko teknis teratas, (c) langkah konkret berikutnya (next action) yang bisa langsung saya kerjakan hari ini.