# Tugas 2 — Arsitektur dan struktur proyek

POC menggunakan satu proses Go. Desain production mempertahankan fase request yang sama sambil menambahkan supervisor master dan worker terpisah. Pembagian master/worker memudahkan privilege separation dan rolling binary upgrade, dengan biaya IPC, cache per-worker, dan koordinasi generasi.

```text
production master [rencana]
  | config global tervalidasi, signal, inherited listen FD, readiness
  +--> worker Go x N [rencana; SO_REUSEPORT/inherited shared socket]
       |
POC: net.Listen -> accept non-blocking -> goroutine koneksi
       -> epoll/netpoll readiness -> read buffer + header deadline
       -> parser HTTP/1.x strict + framing/size limits
       -> validasi decoded path + canonicalize + docroot confinement
       -> .ginxaccess parent -> child (snapshot cached)
       -> ACL sumber -> rewrite -> restart routing + ACL tujuan (maks 8)
       -> content handler (static/index; proxy/FastCGI rencana)
       -> header filter -> response header -> sendfile/plaintext
       -> log/metrics [rencana] -> keep-alive atau close
```

Keputusan ACL sebelum rewrite mencegah rule rewrite mengeluarkan request dari direktori terproteksi; ACL tujuan dijalankan ulang untuk mencegah jalur publik mengarah ke konten privat. Ini lebih ketat dibanding sekadar mengikuti fase rewrite lalu access; konsekuensinya beberapa pola legacy perlu ditulis ulang. Plugin tidak boleh melewati pemeriksaan sumber/tujuan ini.

Struktur berikut adalah **target lengkap**, bukan daftar fitur yang sudah diimplementasikan. `[ada]` berarti file pada POC tersedia; `[rencana]` belum tersedia.

```text
aginx/
  cmd/aginx/main.go                 [ada] CLI, sinyal, lifecycle
  internal/
    core/eventloop/                [rencana] abstraksi readiness; adapter runtime Go
    net/listener/                  [rencana] inherited FD, reuseport, limits
    http1/parser.go                [ada] request line, header, framing, keep-alive
    http2/                         [rencana] stream/frame adapter, flow control
    config/global/                 [rencana] schema global, validator, snapshot
    ginxaccess/
      parser.go                    [ada] lexer baris, parser, compiled rule snapshot
      cache.go                     [ada] metadata identity, TTL, bounded LRU
      lexer.go, ast.go              [rencana] token/span dan AST full grammar
      evaluator.go, merge.go        [rencana] kondisi, budget, inheritance full
      watcher_linux.go             [rencana] inotify + overflow reconciliation
    modules/
      phase.go                     [rencana] registry statis + hook contract
      rewrite/, acl/, headers/      [rencana] pisahkan evaluator dari ginxaccess
      auth/, cachecontrol/          [rencana] credential dan expiry policy
      redirect/, errorpages/        [rencana] external response/subrequest
    static/handler.go              [ada] static file/index, ACL dan rewrite routing
    handlers/fastcgi/, proxy/       [rencana] upstream pool, streaming, timeout
    fsroot/root_linux.go           [ada] descriptor-relative openat + O_NOFOLLOW
    server/server.go               [ada] connection lifecycle, deadline, response
    tls/                           [rencana] certificate/SNI, ALPN, renew snapshot
    logging/                       [rencana] bounded async queue, reopen/rotation
    metrics/                       [rencana] counters/histograms/Prometheus
    process/                       [rencana] master-worker, readiness, binary upgrade
    pool/                          [rencana] bounded buffer pools, request scratch
  internal/**/*_test.go            [ada] unit, protocol integration, fuzz targets
  tests/integration/, security/    [rencana] cross-distro/VM dan attack corpus
  bench/run.sh                     [ada] wrk commands + baseline collection
  bench/nginx.conf                 [ada] baseline konfigurasi Nginx
  bench/results/                   [generated] hasil benchmark aktual
  examples/www/                    [ada] docroot yang dapat langsung dijalankan
  examples/spec/                   [ada] 5 contoh full language (belum didukung POC)
  packaging/systemd/aginx.service   [ada] unit contoh belum dipasang
  packaging/deb/, rpm/             [rencana] build package, metadata, upgrade scripts
  .github/workflows/ci.yml         [ada] test/race/fuzz/static build, belum dieksekusi di GitHub
  docs/                           [ada] Tugas 1–6 dan hasil verifikasi
  go.mod, Makefile                 [ada] reproducible commands tanpa dependency luar
  LICENSE                         [belum dipilih pemilik]
```

Hook internal dirancang sebagai registry **statis saat compile**. Tiap modul mempunyai nama unik, daftar directive, fase, prioritas integer, fungsi validasi config, serta fungsi evaluator. Inputnya RequestContext dan snapshot immutable; hasil enum `Continue`, `Respond`, `Rewrite`, atau `Error`. Modul tidak memiliki socket dan tidak boleh menulis response langsung sebelum header-filter selesai. Prioritas dan nama menjadi tie-break deterministik; error startup untuk registrasi directive ganda. Keuntungannya satu static binary dan ABI sederhana; konsekuensinya menambah modul memerlukan rebuild. Dynamic `.so` dan eksekusi kode dari `.ginxaccess` tidak disediakan.

| Fase | Tanggung jawab dan invariant |
|---|---|
| post-read | Validate authority, request limits, request ID; body belum boleh dialokasikan tanpa batas |
| rewrite | Internal URL rewrite dengan budget dan cycle detection; ACL sumber sudah dicek |
| access | ACL IP, auth, UA, rate limit; berjalan ulang pada tujuan internal routing |
| content | Static, proxy atau FastCGI; file descriptor terkonfinen pada docroot |
| header-filter | Header policy dan framing; Content-Length/TE dikelola transport |
| log | Counter dan bounded log enqueue; tidak menahan koneksi pada disk logging lambat |

Pemisahan fase terinspirasi arsitektur HTTP modular Nginx, tetapi kontrak keamanan AGinX ditetapkan sendiri. [Nginx development guide](https://nginx.org/en/docs/dev/development_guide.html).

Production master membuka listener lalu menurunkan privilege worker; default satu worker Go dengan beberapa CPU. Mode worker per-core memakai `GOMAXPROCS=1` per worker, affinity opsional, dan SO_REUSEPORT melalui `net.ListenConfig.Control`. Jangan menjalankan N worker dengan N `GOMAXPROCS` masing-masing karena oversubscription. Shared listener yang diwariskan mempermudah upgrade tanpa menutup socket; reuseport digunakan untuk listener independen dan perlu uji distribusi koneksi.

Upgrade binary: master memvalidasi binary/config baru, mewariskan listen FD dengan `ExtraFiles`, menunggu readiness worker baru, kemudian menghentikan accept generasi lama dan drain request aktif. Bila readiness gagal, generasi lama terus melayani. PID ownership dan signal routing memerlukan protocol IPC eksplisit. POC hanya mendukung drain SIGTERM/SIGINT dan revalidation cache SIGHUP; mengganti binary dengan restart biasa belum zero downtime.

Buffer per-koneksi POC adalah `bufio.Reader` 4 KiB, tidak ada body buffer unbounded. Standard library memakai strategi pooling internal tertentu; pool milik AGinX baru ditambah setelah profiling. Pool tidak boleh menyimpan oversized input penyerang atau credential antar request. Cache config saat ini mempunyai mutex global untuk index LRU dan mutex per entry; ini aman tetapi dapat menjadi bottleneck multi-core. Target optimasi adalah sharded immutable directory tree dengan lookup berdasarkan path dan generasi.
