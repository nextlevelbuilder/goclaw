package providers

// ShellDenyPatterns are regex patterns for dangerous shell commands.
// Mirrors internal/tools/shell.go defaultDenyPatterns for CLI hook enforcement.
var ShellDenyPatterns = []string{
	// Destructive file operations
	`\brm\s+-[rf]{1,2}\b`,
	`\brm\s+.*--recursive`,
	`\brm\s+.*--force`,
	`\bdel\s+/[fq]\b`,
	`\brmdir\s+/s\b`,
	`\b(mkfs|diskpart)\b|\bformat\s`,
	`\bdd\s+if=`,
	`>\s*/dev/sd[a-z]\b`,
	`\b(shutdown|reboot|poweroff)\b`,
	`:\(\)\s*\{.*\};\s*:`,

	// Data exfiltration
	`\bcurl\b.*\|\s*(ba)?sh\b`,
	`\bcurl\b.*(-d\b|-F\b|--data|--upload|--form|-T\b|-X\s*P(UT|OST|ATCH))`,
	`\bwget\b.*-O\s*-\s*\|\s*(ba)?sh\b`,
	`\bwget\b.*--post-(data|file)`,
	`\b(nslookup|dig|host)\b`,
	`/dev/tcp/`,

	// Reverse shells
	`\b(nc|ncat|netcat)\b.*-[el]\b`,
	`\bsocat\b`,
	`\bopenssl\b.*s_client`,
	`\btelnet\b.*[0-9]+`,
	`\bpython[23]?\b.*\bimport\s+(socket|http\.client|urllib|requests)\b`,
	`\bperl\b.*-e\s*.*\b[Ss]ocket\b`,
	`\bruby\b.*-e\s*.*\b(TCPSocket|Socket)\b`,
	`\bnode\b.*-e\s*.*\b(net\.connect|child_process)\b`,
	`\bawk\b.*/inet/`,
	`\bmkfifo\b`,

	// Dangerous eval / code injection
	`\beval\s*\$`,
	`\bbase64\s+-d\b.*\|\s*(ba)?sh\b`,

	// Privilege escalation
	`\bsudo\b`,
	`\bsu\s+-`,
	`\bnsenter\b`,
	`\bunshare\b`,
	`\b(mount|umount)\b`,
	`\b(capsh|setcap|getcap)\b`,

	// Dangerous path operations
	`\bchmod\s+[0-7]{3,4}\s+/`,
	`\bchown\b.*\s+/`,
	`\bchmod\b.*\+x.*/tmp/`,
	`\bchmod\b.*\+x.*/var/tmp/`,
	`\bchmod\b.*\+x.*/dev/shm/`,

	// Environment variable injection
	`\bLD_PRELOAD\s*=`,
	`\bDYLD_INSERT_LIBRARIES\s*=`,
	`\bLD_LIBRARY_PATH\s*=`,
	`/etc/ld\.so\.preload`,
	`\bGIT_EXTERNAL_DIFF\s*=`,
	`\bGIT_DIFF_OPTS\s*=`,
	`\bBASH_ENV\s*=`,
	`\bENV\s*=.*\bsh\b`,

	// Container escape
	`/var/run/docker\.sock|docker\.(sock|socket)`,
	`/proc/sys/(kernel|fs|net)/`,
	`/sys/(kernel|fs|class|devices)/`,

	// Crypto mining
	`\b(xmrig|cpuminer|minerd|cgminer|bfgminer|ethminer|nbminer|t-rex|phoenixminer|lolminer|gminer|claymore)\b`,
	`stratum\+tcp://|stratum\+ssl://`,

	// Filter bypass (CVE-2025-66032)
	`\bsed\b.*['"]/e\b`,
	`\bsort\b.*--compress-program`,
	`\bgit\b.*(--upload-pack|--receive-pack|--exec)=`,
	`\b(rg|grep)\b.*--pre=`,
	`\bman\b.*--html=`,
	`\bhistory\b.*-[saw]\b`,
	`\$\{[^}]*@[PpEeAaKk]\}`,

	// Network abuse / reconnaissance
	`\b(nmap|masscan|zmap|rustscan)\b`,
	`\b(ssh|scp|sftp)\b.*@`,
	`\b(chisel|frp|ngrok|cloudflared|bore|localtunnel)\b`,

	// Persistence
	`\bcrontab\b`,
	`>\s*~/?\.(bashrc|bash_profile|profile|zshrc)`,
	`\btee\b.*\.(bashrc|bash_profile|profile|zshrc)`,

	// Process manipulation
	`\bkill\s+-9\s`,
	`\b(killall|pkill)\b`,

	// Environment variable dumping
	`^\s*env\s*$`,
	`^\s*env\s*\|`,
	`^\s*env\s*>\s`,
	`\bprintenv\b`,
	`^\s*(set|export\s+-p|declare\s+-x)\s*($|\|)`,
	`\bcompgen\s+-e\b`,
	`/proc/[^/]+/environ`,
	`/proc/self/environ`,
	`(?i)\bstrings\b.*/proc/`,
}
