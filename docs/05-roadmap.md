# Tugas 5 — Roadmap dan checklist

Estimasi untuk satu engineer berpengalaman, fokus penuh, termasuk review dan test; rentang sekitar **25–41 minggu**, HTTP/3 tambahan 4–8 minggu. Integrasi protocol dan audit eksternal dapat memperpanjangnya. `[x]` hanya menandai artefak/fungsi yang sudah ada, bukan sertifikasi production. Tidak semua item sebuah fase wajib selesai sebelum pekerjaan fase lain dapat dimulai.

## Fase 0 — Fondasi, 1 minggu

- [x] Keputusan bahasa Go dan toolchain deklaratif.
- [x] Source tree, Makefile, coding standard `gofmt`, `go vet`.
- [x] Test harness unit, integration, race, dan fuzz.
- [x] Workflow GitHub Actions tersedia, dengan TCP wajib.
- [ ] Inisialisasi repository Git nyata dan aktifkan CI di GitHub; metadata Git belum tersedia di workspace ini.
- [ ] Pemilik menetapkan lisensi dan kebijakan kontribusi.
- [ ] CI berjalan sukses pada stable/oldstable serta dependency/toolchain vulnerability scan.

## Fase 1 — Core Engine, 3–4 minggu

- [x] Listener non-blocking + event loop melalui runtime netpoller.
- [ ] Master-worker & SO_REUSEPORT, inherited listener FD, readiness protocol.
- [ ] Buffer pool bounded berdasarkan allocation profiling.
- [x] Header, idle/keep-alive, dan write timeout.
- [x] Graceful shutdown socket dan SIGHUP cache revalidation.
- [ ] Global config reload atomic dengan rollback dan multi-worker generation.
- [ ] Graceful binary upgrade dengan listener inheritance dan rollback.
- [ ] Pool/limiter disk I/O dan backpressure ketika filesystem dingin/lambat.

## Fase 2 — HTTP Layer, 3–5 minggu

- [x] Parser HTTP/1.1 minimal strict dengan penolakan framing ambigu pada subset tanpa body.
- [x] Keep-alive & sequential pipelining GET/HEAD.
- [ ] Parser full body/framing, differential fuzz RFC 9112 sebelum upstream support.
- [ ] Range request termasuk 206/416 dan validasi multipart.
- [ ] Chunked encoding, trailer limits, dan request body streaming terbatas.
- [ ] ETag/Last-Modified/304; POC baru mengirim Last-Modified.
- [ ] Gzip/Brotli dengan precompressed asset dan Vary yang konsisten.
- [ ] Absolute-form/CONNECT policy dan authority validation untuk mode proxy bila ditambahkan.

## Fase 3 — Static Serving, 2–3 minggu

- [x] Jalur `io.CopyN(TCPConn, File)` yang memakai sendfile bila didukung.
- [ ] Bukti syscall sendfile di mesin tanpa pembatasan socket/ptrace.
- [x] MIME type dari extension, fallback octet-stream, nosniff.
- [x] Fixed directory index `index.html`, directory listing off.
- [ ] Configurable directory index dan canonical directory redirect.
- [ ] Open-file cache bounded, invalidation content, descriptor lifetime.
- [ ] Custom error pages dan header-filter pada error response.

## Fase 4 — `.ginxaccess` Engine, 4–6 minggu

- [x] Lexer/parser subset dan spesifikasi full EBNF.
- [x] Compiled config immutable dan evaluator ACL/header/rewrite minimal.
- [x] Bounded LRU + metadata TTL + negative cache.
- [ ] AST full grammar dengan source span, semantic typing, conditional blocks.
- [ ] Cache + inotify watcher, overflow recovery, directory rename reconciliation.
- [x] Inheritance minimum: ACL parent tidak dapat dilonggarkan, header child override.
- [ ] Full inheritance/merge serta allow-override policy global.
- [x] Modul minimal rewrite, ACL, header set.
- [ ] Auth, redirect, header add/unset, error-pages, UA, hotlink, rate limit, cache-control.
- [x] Validator CLI `aginx -t -config file` untuk subset POC.
- [x] Fuzzing parser smoke.
- [ ] Fuzzing berkelanjutan dengan persisted corpus dan budget evaluasi full engine.
- [ ] Ukur overhead cache-hit dan capai <5% pada workload terdefinisi.

## Fase 5 — Keamanan, 4–6 minggu

- [ ] TLS 1.2/1.3 + SNI, ALPN, certificate renewal snapshot.
- [x] Batas header/koneksi, timeout, traversal/symlink protection, anti-slowloris dasar.
- [x] Security header default `X-Content-Type-Options: nosniff`.
- [ ] HSTS hanya pada HTTPS dan policy CSP/referrer sesuai aplikasi.
- [ ] Privilege drop master-worker, filesystem read-only, chroot/seccomp setelah syscall profiling.
- [ ] Audit eksternal parser/lifecycle dan fuzz malformed HTTP jangka panjang.
- [ ] ASan/TSan untuk dependency C/unsafe jika kelak ditambahkan; Go race suite tersedia.
- [ ] Soak 24 jam, chaos FD/memory pressure, disk stalls, watcher overflow.
- [ ] Target 100K koneksi dan memory per idle connection dibuktikan, bukan hanya dinaikkan limitnya.

## Fase 6 — Fitur Lanjutan, 5–10 minggu

- [ ] HTTP/2: stream limits, HPACK, flow control, rapid-reset mitigation.
- [ ] Reverse proxy/FastCGI (PHP-FPM): streaming, upstream timeout, retry policy aman.
- [ ] Virtual host dengan authority/SNI consistency dan isolation docroot.
- [ ] Access/error log + reopen/rotasi, bounded log queue.
- [ ] Metrics Prometheus dengan bounded cardinality dan endpoint admin terpisah.
- [ ] HTTP/3/QUIC opsional, tambahan 4–8 minggu; setelah HTTP/2 stabil.

## Fase 7 — Packaging dan Rilis, 3–6 minggu

- [x] Static binary Linux, verifikasi ELF lokal.
- [ ] .deb Debian/Ubuntu: conffile, user service, upgrade/rollback, uninstall bersih.
- [ ] .rpm AlmaLinux/RHEL-family: SELinux policy, sysusers, systemd scriptlets.
- [x] systemd unit contoh dengan non-root `DynamicUser`, belum dipasang/diuji operasional.
- [ ] Uji Debian, Ubuntu, AlmaLinux, dan RHEL-family/CentOS Stream pada VM dengan kernel asli.
- [ ] Kernel 4.18 dan distro tua diuji eksplisit; container saja berbagi kernel host dan tidak membuktikannya.
- [x] Dokumentasi desain, build, spesifikasi, roadmap, benchmark plan.
- [ ] Man page, operator runbook, signed release, SBOM, release artifacts.
- [ ] Benchmark report AGinX vs Nginx/Apache pada hardware identik dan konfigurasi setara.

Keputusan arsitektur: Go, runtime epoll, descriptor-relative static serving, snapshot policy immutable, ACL yang tidak dapat dilonggarkan child. Risiko terbesar: memory/GC 100K koneksi, cold disk, semantics rewrite/auth, konsistensi watcher multi-worker, serta parser/protocol attack surface.

Next action hari ini: jalankan `make check`, mulai `./bin/aginx -root examples/www`, uji curl serta atomic config update, lalu jalankan baseline `-access=false` dan `-access=true` pada mesin yang mengizinkan socket. Setelah itu prioritaskan inotify dan benchmark overhead sebelum HTTP/2/FastCGI.
