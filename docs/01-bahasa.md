# Tugas 1 — Rekomendasi bahasa

Keputusan final: **Go**, dengan socket non-blocking dan `epoll` melalui runtime netpoller. AGinX tetap binary native mandiri; ketergantungan pada standard library tidak membuatnya menjadi proxy Apache/Nginx.

Asumsi eksplisit: tim awal kecil, prioritas pertama POC yang dapat diaudit; docroot di local filesystem dan ditulis publisher terpercaya; HTTP plaintext pada loopback untuk POC; satu proses menggunakan CPU melalui scheduler Go. Target master-worker diwujudkan sesudah baseline satu proses terukur. Tidak ada angka benchmark bahasa yang diasumsikan sebagai hasil AGinX.

Matriks berikut adalah penilaian engineering, bukan benchmark. Hasil p99 bergantung workload, allocator, filesystem, TLS, dan konfigurasi kernel.

| Kriteria | C | Go | Rust |
|---|---|---|---|
| Throughput potensial | Sangat tinggi, kontrol layout/pool penuh | Tinggi; alokasi dan GC perlu diprofil | Sangat tinggi; kontrol layout dan ownership |
| Tail latency p99 | Tanpa GC; allocator/page fault tetap dapat spike | GC assist, scheduler, dan allocation rate perlu diukur | Tanpa tracing GC; executor, allocator dan blocking task tetap berpengaruh |
| Memory idle connection | Paling mudah dipadatkan dengan struct/state machine | Goroutine, stack, reader dan metadata lebih besar | Future/state machine dapat kompak; ukuran bergantung implementasi |
| GC pause | Tidak ada tracing GC | Ada GC concurrent dan fase stop-the-world; bukan jaminan pause nol | Tidak ada tracing GC; destructor besar bisa menambah latency |
| Memory safety | Manual; UAF/overflow/race berisiko tinggi | Memory-safe pada kode biasa; race dan unsafe tetap berisiko | Safe Rust melindungi ownership/data race; audit unsafe/FFI tetap perlu |
| Kecepatan development | Parser/lifecycle manual memperlama audit | Paling cepat untuk tim kecil; standard library luas | Sedang; ownership pada state I/O rumit memerlukan pengalaman |
| TLS/HTTP/2/HTTP/3 | Library matang tetapi FFI/dependency dan build rumit | TLS native, dukungan HTTP/2; HTTP/3 perlu library tambahan | rustls/hyper/quinn sebagai dependency; integrasi dan static target perlu audit |
| Static build | musl praktis, library TLS/QUIC perlu diselaraskan | `CGO_ENABLED=0`; tidak ada dependency eksternal pada POC | musl/crt-static; dependency native dapat memerlukan toolchain tambahan |
| Packaging .deb/.rpm | Mudah setelah ABI/dependency ditetapkan | Sederhana: satu binary + unit + config | Sederhana untuk binary static; native dependency perlu pemeriksaan |

Rust menjamin banyak properti memory safety pada safe subset, tetapi `unsafe` memerlukan invariant manual. Static linkage harus mencakup dependency native, bukan hanya crate. [Rust Reference: unsafe](https://doc.rust-lang.org/reference/unsafety.html), [linkage](https://doc.rust-lang.org/reference/linkage.html).

Alasan memilih Go: standard library mengurangi kode infrastruktur, race detector/fuzzer tersedia, dan single static binary mudah dihasilkan. Biayanya: memory idle lebih tinggi dan GC membuat target p99 harus dibuktikan. Mitigasi: snapshot config immutable, pembatasan cache/koneksi, pool buffer terbatas, profiling allocation, `GOMEMLIMIT` dengan headroom native/kernel, serta perbandingan baseline identik. Jangan mematikan GC tanpa model lifetime yang terukur. [Go GC guide](https://go.dev/doc/gc-guide).

Target awal userspace idle: **12–24 KiB per koneksi**, diukur dari slope RSS sesudah warm-up terhadap 0/10K/50K/100K koneksi. Angka ini adalah budget awal, bukan hasil. Pada 100K koneksi berarti sekitar 1,2–2,4 GiB di luar memory dasar proses dan kernel socket buffers. POC membatasi 10K secara default. C/Rust dengan state machine kecil berpotensi menurunkan budget, tetapi keputusan pindah bahasa hanya setelah profiling menunjukkan bottleneck yang tidak layak diperbaiki di Go.

Go `net.Listener` dan `net.TCPConn` mendaftarkan descriptor non-blocking ke netpoller. Saat `Read`/`Write` menunggu kesiapan, goroutine diparkir; scheduler dapat menjalankan goroutine lain. `GOMAXPROCS` adalah parallelism scheduler, bukan jumlah worker process. [Backend epoll runtime Go](https://go.dev/src/runtime/netpoll_epoll.go).

Pada C/Rust event loop langsung, syscall `stat/open`, DNS sinkron, serta disk read cache-miss tidak boleh dijalankan pada thread event loop: gunakan pool disk terbatas, resolver asynchronous, queue bounded, deadline dan backpressure. Pada Go, syscall file biasa tetap bisa memblokir OS thread meskipun scheduler dapat menjadwalkan pekerjaan lain. `sendfile` juga dapat menunggu page fault/disk; zero-copy tidak berarti disk asynchronous. POC memakai operasi file sinkron pada goroutine koneksi dan hanya local filesystem; production menambahkan semaphore/pool untuk file/config I/O agar cold disk tidak menghasilkan pertumbuhan thread tanpa batas.

Data plaintext file dikirim dengan `io.CopyN(*net.TCPConn, *os.File, size)`. Jalur standard library mempertahankan fast path `sendfile`, dengan fallback bila kernel/filesystem tidak mendukung. TLS userspace atau compression membutuhkan jalur berbeda; jangan mengklaim zero-copy setelah transformasi. [Implementasi sendfile Go](https://go.dev/src/net/sendfile.go).

Risiko utama keputusan ini adalah GC/memory pada 100K koneksi dan thread yang menunggu disk. Langkah pertama: jalankan build/test dan benchmark baseline sebelum menambah protocol atau optimasi allocator.
