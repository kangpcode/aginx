// Package ginxaccess mengompilasi subset konfigurasi menjadi snapshot immutable.
package ginxaccess

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"aginx/internal/http1"
)

const MaxFileBytes = 64 << 10
const MaxRules = 256

type ACLRule struct {
	Allow   bool
	All     bool
	Network netip.Prefix
}
type HeaderRule struct{ Name, Value string }
type RewriteRule struct {
	Pattern *regexp.Regexp
	Target  string
}

// Apply mengganti match pertama; ekspansi capture dibatasi sebelum hasil membesar.
func (r RewriteRule) Apply(source string) (string, bool, error) {
	indexes := r.Pattern.FindStringSubmatchIndex(source)
	if indexes == nil {
		return "", false, nil
	}
	var out strings.Builder
	appendPart := func(part string) error {
		if out.Len()+len(part) > 8192 {
			return fmt.Errorf("hasil rewrite melebihi 8 KiB")
		}
		out.WriteString(part)
		return nil
	}
	if err := appendPart(source[:indexes[0]]); err != nil {
		return "", true, err
	}
	for i := 0; i < len(r.Target); {
		start := i
		if r.Target[i] != '$' {
			for i < len(r.Target) && r.Target[i] != '$' {
				i++
			}
			if err := appendPart(r.Target[start:i]); err != nil {
				return "", true, err
			}
			continue
		}
		i++
		if i < len(r.Target) && r.Target[i] == '$' {
			i++
		} else if i < len(r.Target) && r.Target[i] == '{' {
			end := strings.IndexByte(r.Target[i:], '}')
			if end >= 0 {
				i += end + 1
			}
		} else {
			for i < len(r.Target) {
				b := r.Target[i]
				if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_') {
					break
				}
				i++
			}
		}
		// Satu capture maksimal sepanjang input, sehingga alokasi sementara tetap terbatas.
		expanded := r.Pattern.ExpandString(nil, r.Target[start:i], source, indexes)
		if err := appendPart(string(expanded)); err != nil {
			return "", true, err
		}
	}
	if err := appendPart(source[indexes[1]:]); err != nil {
		return "", true, err
	}
	return out.String(), true, nil
}

type Config struct {
	ACL      []ACLRule
	Headers  []HeaderRule
	Rewrites []RewriteRule
}

// Allowed memakai first-match dalam satu direktori; default deny bila ada ACL.
func (c *Config) Allowed(ip netip.Addr) bool {
	if c == nil || len(c.ACL) == 0 {
		return true
	}
	ip = ip.Unmap()
	for _, rule := range c.ACL {
		if rule.All || rule.Network.Contains(ip) {
			return rule.Allow
		}
	}
	return false
}

// lexLine menerima token telanjang atau string Go berpetik ganda, diakhiri ';'.
// Tidak ada ekspansi variabel, evaluasi kode, include, atau shell.
func lexLine(line string) ([]string, error) {
	var tokens []string
	ended := false
	for i := 0; i < len(line); {
		if line[i] == ' ' || line[i] == '\t' || line[i] == '\r' {
			i++
			continue
		}
		if line[i] == '#' {
			break
		}
		if ended {
			return nil, fmt.Errorf("teks setelah ';'")
		}
		if line[i] == ';' {
			ended = true
			i++
			continue
		}
		if line[i] == '"' {
			start := i
			i++
			closed := false
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("string tidak ditutup")
			}
			value, err := strconv.Unquote(line[start:i])
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, value)
			if i < len(line) && !strings.ContainsRune(" \t\r;#", rune(line[i])) {
				return nil, fmt.Errorf("pemisah token tidak valid")
			}
		} else {
			start := i
			for i < len(line) && !strings.ContainsRune(" \t\r;#", rune(line[i])) {
				i++
			}
			token := line[start:i]
			if strings.ContainsAny(token, "\"{}<>") {
				return nil, fmt.Errorf("token tidak valid")
			}
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != 0 && !ended {
		return nil, fmt.Errorf("wajib ';' di akhir directive")
	}
	return tokens, nil
}

func Parse(data []byte) (*Config, error) {
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("baris 1: file melebihi 64 KiB")
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return nil, fmt.Errorf("baris 1: UTF-8/NUL tidak valid")
	}
	cfg := &Config{}
	count := 0
	for i, line := range strings.Split(string(data), "\n") {
		tokens, err := lexLine(line)
		if err == nil && len(tokens) != 0 {
			count++
			if count > MaxRules {
				err = fmt.Errorf("maksimum %d directive", MaxRules)
			} else {
				err = cfg.add(tokens)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("baris %d: %w", i+1, err)
		}
	}
	return cfg, nil
}

func (c *Config) add(t []string) error {
	switch t[0] {
	case "allow", "deny":
		if len(t) != 2 {
			return fmt.Errorf("%s membutuhkan IP/CIDR/all", t[0])
		}
		rule := ACLRule{Allow: t[0] == "allow", All: t[1] == "all"}
		if !rule.All {
			prefix, err := netip.ParsePrefix(t[1])
			if err != nil {
				ip, e := netip.ParseAddr(t[1])
				if e != nil || ip.Zone() != "" {
					return fmt.Errorf("IP/CIDR tidak valid")
				}
				ip = ip.Unmap()
				prefix = netip.PrefixFrom(ip, ip.BitLen())
			}
			if prefix.Addr().Is4In6() {
				return fmt.Errorf("gunakan CIDR IPv4 biasa")
			}
			rule.Network = prefix.Masked()
		}
		c.ACL = append(c.ACL, rule)
	case "header":
		if len(t) != 4 || t[1] != "set" || !http1.Token(t[2]) || !http1.Value(t[3]) {
			return fmt.Errorf("format: header set Nama \"Nilai\";")
		}
		name := http.CanonicalHeaderKey(t[2])
		switch name {
		case "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "Upgrade", "Keep-Alive", "Proxy-Connection", "Date", "Server":
			return fmt.Errorf("header %s dikelola server", name)
		}
		c.Headers = append(c.Headers, HeaderRule{name, t[3]})
	case "rewrite":
		if len(t) != 4 || t[3] != "last" {
			return fmt.Errorf("format: rewrite \"regex\" \"/target\" last;")
		}
		if len(t[1]) > 1024 || !strings.HasPrefix(t[2], "/") || len(t[2]) > 8192 || !http1.Value(t[2]) {
			return fmt.Errorf("regex/target rewrite tidak valid")
		}
		re, err := regexp.Compile(t[1])
		if err != nil {
			return err
		}
		c.Rewrites = append(c.Rewrites, RewriteRule{re, t[2]})
	default:
		return fmt.Errorf("directive tidak dikenal: %s", t[0])
	}
	return nil
}
