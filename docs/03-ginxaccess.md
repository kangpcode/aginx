# Tugas 3 — Spesifikasi `.ginxaccess`

Language v1 ditetapkan di dokumen ini. **Parser POC hanya mengimplementasikan `allow`, `deny`, `header set`, dan `rewrite ... last`**, satu directive per baris. Directive atau block lain ditolak dengan nomor baris, bukan diabaikan. Kelima contoh full language di bawah adalah spesifikasi untuk fase berikutnya.

Sintaks case-sensitive, UTF-8, komentar `#` di luar string, directive diakhiri `;`. String berpetik ganda mengikuti escape Go (`\n`, `\t`, `\"`, `\\`, dan Unicode escape); regex dalam string harus menggandakan backslash. Tidak ada interpolation shell/environment atau eksekusi kode. Match rewrite menggunakan path decoded absolut dari docroot, termasuk `/`, pada setiap level; berbeda dari penghilangan prefix per-direktori Apache. Ini menghindari arti regex yang berubah ketika file dipindahkan.

EBNF full language:

```ebnf
file         = { line } ;
line         = blank | comment, eol | directive, eol | block ;
directive    = identifier, { ws, argument }, [ ws ], ";", [ ws, comment ] ;
block        = if_block | files_block | match_block ;
if_block     = "<If", ws, expression, [ ws ], ">", eol,
               { line }, "</If>", eol ;
files_block  = "<Files", ws, string, [ ws ], ">", eol,
               { line }, "</Files>", eol ;
match_block  = "<Match", ws, string, [ ws ], ">", eol,
               { line }, "</Match>", eol ;
expression   = conjunction, { ws, "or", ws, conjunction } ;
conjunction  = unary, { ws, "and", ws, unary } ;
unary        = [ "not", ws ], predicate | "(", expression, ")" ;
predicate    = "file_exists" | "dir_exists"
             | field, ws, operator, ws, string ;
field        = "method" | "path" | "host" | "user_agent" | "referer" ;
operator     = "==" | "!=" | "~" | "!~" ;
argument     = string | bare ;
string       = '"', { utf8_nonquote_noncontrol | escape }, '"' ;
escape       = "\\", ( '"' | "\\" | "n" | "r" | "t" | "a" | "b" | "f" | "v"
             | "x", hex, hex | "u", hex, hex, hex, hex
             | "U", hex, hex, hex, hex, hex, hex, hex, hex
             | octal, octal, octal ) ;
identifier   = letter, { letter | digit | "_" } ;
bare         = bare_char, { bare_char } ;
bare_char    = ? UTF-8 scalar selain whitespace, control, '"', '#', ';', '<', '>', '{', '}' ? ;
blank        = { ws }, eol ;
comment      = "#", { non_eol } ;
ws           = ( " " | "\t" ), { " " | "\t" } ;
eol          = "\n" | "\r\n" ;
letter       = "a" .. "z" | "A" .. "Z" ;
digit        = "0" .. "9" ;
hex          = digit | "a" .. "f" | "A" .. "F" ;
octal        = "0" .. "7" ;
```

Indentasi awal diabaikan; lexer menambahkan newline virtual bila file terakhir tidak memilikinya. `utf8_nonquote_noncontrol` mengecualikan backslash; `non_eol` adalah karakter selain CR/LF. AST menyimpan file, line, column, dan original source span. Semantic validator memeriksa aritas dan tipe setiap directive setelah parsing. CLI melaporkan misalnya `admin/.ginxaccess:7:1: allow: CIDR tidak valid`. POC mengembalikan `baris 7: ...` dan logger menambahkan path.

| Directive v1 | Semantik, default, dan pembatasan |
|---|---|
| `allow IP/CIDR/all;`, `deny IP/CIDR/all;` | First-match default; jika ada ACL tetapi tak ada match, deny. IP peer socket, X-Forwarded-For tidak dipercaya |
| `acl_order first_match;` / `deny_overrides;` | First match sesuai urutan teks, atau setiap matching deny menang; default first_match. Hanya dalam scope yang sama |
| `rewrite "regex" "/target" last;` / `break;` | `last` internal routing ulang; `break` berhenti rewrite pada pass tersebut tetapi ACL tujuan tetap wajib. Capture `${1}`, `${name}`, `$$` sesuai ekspansi Go; hanya match pertama diganti |
| `redirect 301/302/307/308 "regex" "target";` | Location tervalidasi; URL absolut hanya `https`/`http` dan allowlist host global; tidak boleh CR/LF atau scheme relatif `//` |
| `header set/add/unset Nama ["nilai"];` | Operasi berurutan parent→child; nama token, nilai bebas CR/LF. Framing dan hop-by-hop dikelola server |
| `error_page 404 "/errors/404.html";` | Internal content lookup; status asli dipertahankan, ACL target tetap berlaku, satu error subrequest saja |
| `auth_basic "Realm";`, `auth_user_file "credential-id";` | Credential-id merujuk file global di luar docroot; salted password hash, constant-time compare, TLS wajib di production; autentikasi parent tidak dapat dinonaktifkan child |
| `cache_control "text/css" "public, max-age=86400";` | MIME exact atau wildcard `image/*`; child mengganti key MIME yang sama |
| `expires "image/*" 86400;` | Detik nonnegatif dari response time; Cache-Control eksplisit lebih diprioritaskan |
| `user_agent deny/allow "regex";` | First-match; presence policy memakai default deny bila ada allowlist, selain itu allow. Identitas UA dapat dipalsukan |
| `rate_limit 10 20;` | Token bucket 10 request/s/IP, burst 20; bounded table + TTL, IPv6 prefix configurable global; overflow tabel menghasilkan 429 |
| `directory_index index.html index.htm;` | Child mengganti daftar; nama file biasa, bukan path. POC fixed index.html |
| `directory_listing off;` | Default off; child tidak boleh mengaktifkan bila global mengunci off |
| `hotlink allow_empty on;`, `hotlink_allow "^https://...";` | Match Referer RE2; bukan mekanisme auth karena header dapat dipalsukan |
| `respond 503;` | Terminal response; status 4xx/5xx allowlist. Custom error_page dapat menyediakan body |
| `include "relative.conf";` | Rencana opsional; hanya subtree config yang diizinkan global, no symlink, cycle detection, kedalaman maksimal 4; POC menolak include |

`<Files "*.css">` mencocokkan basename dengan glob tanpa separator. `<Match "^/api/">` mencocokkan path decoded absolut dengan RE2. `<If>` hanya mempunyai predikat di grammar; tidak ada arbitrary expression evaluator. `file_exists`/`dir_exists` memakai target terkonfinen saat pass tersebut dan budget I/O, bukan filesystem global. Block yang match dievaluasi sesuai urutan teks pada fase directive terkait; `or` lebih rendah daripada `and`. Kondisi header divalidasi agar tidak membocorkan nilai credential ke log.

Inheritance selalu menelusuri docroot sampai direktori target. ACL adalah **AND antar layer**: child tidak dapat membuka akses yang ditolak parent. Header berurutan parent lalu child; `set` mengganti key, `add` menambah value, `unset` menghapus kecuali header yang dikunci global. Rewrite digabung parent→child; rule matching pertama menghentikan pass untuk `last`, sehingga parent catch-all dapat menutupi rule child dan perlu kondisi/regex yang sempit. Auth bertambah ketat; error-page/cache mapping dan directory index boleh override per key. Rate-limit parent dan child masing-masing harus lolos. Kebijakan ini menukar kompatibilitas `.htaccess` dengan batas delegasi yang lebih mudah diaudit.

Batas keamanan v1: 64 KiB per file dan 256 directive/file; full language membatasi nesting block 8, include 4 dan total expanded source 256 KiB; maksimal 32 komponen path plus terminal slash; 8 internal transitions, decoded path/output rewrite 8 KiB, regex source 1024 byte, dan maksimal 4096 evaluasi regex/request. Go regexp mempunyai waktu linear terhadap ukuran input untuk pola tetap; tidak ada backreference/lookaround PCRE. [Dokumentasi regexp](https://pkg.go.dev/regexp).

Budget POC 20 ms diperiksa di antara evaluasi rewrite, bukan hard preemption terhadap sebuah regex atau syscall file. Full engine harus mempertahankan batas input/AST dan cooperative budget untuk seluruh fase; pola tunggal tetap harus selesai sebelum pengecekan berikutnya. Timeout dengan meluncurkan goroutine lalu meninggalkannya tidak digunakan karena menghabiskan resource. ACL/auth baru yang rusak tanpa snapshot lama harus fail closed.

Cache production mengindeks canonical relative path dengan identity `(device, inode, mtime_ns, ctime_ns, size)` dan generasi parent. Metadata membedakan versi; inotify menyegarkan lebih cepat dan menangkap perubahan yang timestamp-nya dipertahankan. Watch **direktori** untuk create/delete/rename/close-write, bukan hanya inode file. Saat `IN_Q_OVERFLOW`, directory move, atau watcher failure, tandai subtree stale dan rescan. Watcher tidak recursive otomatis; daftar direktori dibatasi dan direkonsiliasi. [inotify manual](https://man7.org/linux/man-pages/man7/inotify.7.html).

POC mengimplementasikan bounded LRU 1024 entry termasuk negative cache dan metadata TTL default 100 ms pada request, tanpa inotify. Cache hit sebelum TTL tidak melakukan open/stat/parse, tetapi masih ada traversal policy dan open/stat content. Snapshot diganti atomik di bawah mutex per entry. Parse/read gagal menggunakan snapshot valid terakhir yang masih tersimpan dan mencatat error; jika tidak ada snapshot, request 503. Eviction kehilangan last-known-good sehingga file rusak setelah eviction akan fail closed. Deletion file adalah penghapusan policy eksplisit setelah TTL; gunakan atomic rename agar penulisan parsial tidak dianggap konfigurasi valid kosong. SIGHUP mempertahankan last-good dan memaksa pemeriksaan metadata berikutnya, tanpa menutup koneksi.

Lima contoh lengkap berada di `examples/spec/`:

`examples/spec/wordpress.ginxaccess` — memerlukan FastCGI dan block evaluator.

```nginx
directory_index index.php index.html;
directory_listing off;
<If not file_exists and not dir_exists>
    rewrite "^/.*$" "/index.php" last;
</If>
```

`examples/spec/api.ginxaccess` — ACL, UA, dan rate-limit berlapis.

```nginx
allow 192.0.2.0/24;
allow 2001:db8:1234::/48;
deny all;
header set X-Content-Type-Options "nosniff";
user_agent deny "(?i)(scanner|sqlmap)";
rate_limit 20 40;
directory_listing off;
```

`examples/spec/assets.ginxaccess` — caching, matching file, dan hotlink.

```nginx
directory_listing off;
cache_control "text/css" "public, max-age=86400";
cache_control "application/javascript" "public, max-age=86400";
expires "image/*" 604800;
hotlink allow_empty on;
hotlink_allow "^https://(www[.])?example[.]com/";
<Files "*.map">
    deny all;
</Files>
```

`examples/spec/admin.ginxaccess` — IP allowlist dan Basic Auth.

```nginx
allow 10.20.0.0/16;
deny all;
auth_basic "Administrasi";
auth_user_file "admin-users";
header set Cache-Control "no-store";
<Match "^/admin/legacy$">
    redirect 308 "^/admin/legacy$" "/admin/";
</Match>
```

`examples/spec/maintenance.ginxaccess` — error body dengan status 503.

```nginx
error_page 503 "/maintenance.html";
header set Retry-After "600";
<If path !~ "^/(maintenance[.]html|assets/)" >
    respond 503;
</If>
```

Untuk POC yang langsung berjalan, gunakan `examples/www/.ginxaccess`. POC menolak ekstensi `.php`, `.phtml`, `.phar`, `.cgi`, dan `.fcgi` tanpa membedakan huruf besar/kecil. Ini bukan daftar lengkap seluruh format source aplikasi; gunakan docroot khusus asset static sampai FastCGI dan handler mapping tersedia.
