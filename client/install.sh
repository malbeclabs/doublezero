#!/usr/bin/env bash
#
# DoubleZero Edge installer
# -------------------------
# Served from https://get.doublezero.xyz/install and run as:
#
#     curl -fsSL https://get.doublezero.xyz/install | bash
#
# It checks for Docker (offering to install it), preps the host for GRE,
# loads the user's keypair, runs the thin doublezero client container
# (ghcr.io/malbeclabs/doublezero), and connects.
#
# Non-interactive overrides (env vars):
#   DZ_ENV=testnet|devnet|mainnet-beta   default: prompt (falls back to mainnet-beta)
#   DZ_KEYPAIR=/abs/path/to/id.json      default: prompt
#   DZ_CONNECT="connect multicast"       connect args to run (default: "connect multicast")
#   DZ_IMAGE=ghcr.io/malbeclabs/doublezero:latest
#   DZ_NAME=doublezero                   container name
#   DZ_ASSUME_YES=1                      skip confirmation prompts (e.g. Docker install)
#
# NOTE: connecting requires the host's public IP to have an access pass / allowlisted
# user onchain for the chosen environment. That provisioning is a separate step; if
# `connect` reports an access-pass error, the rest of the setup is still in place.

set -euo pipefail

# ----------------------------------------------------------------------------
# config / defaults
# ----------------------------------------------------------------------------
DZ_IMAGE="${DZ_IMAGE:-ghcr.io/malbeclabs/doublezero:latest}"
DZ_NAME="${DZ_NAME:-doublezero}"
DZ_ENV="${DZ_ENV:-}"
DZ_KEYPAIR="${DZ_KEYPAIR:-}"
DZ_CONNECT="${DZ_CONNECT:-}"
DZ_ASSUME_YES="${DZ_ASSUME_YES:-0}"
KEYPAIR_DEST="/root/.config/doublezero/id.json"   # client's default keypair path (container runs as root)
LIVENESS_UDP_PORT=44880

# ----------------------------------------------------------------------------
# pretty output + prompts (read from the terminal, not the curl pipe)
# ----------------------------------------------------------------------------
if [ -t 1 ]; then BOLD=$'\033[1m'; RED=$'\033[31m'; YEL=$'\033[33m'; GRN=$'\033[32m'; RST=$'\033[0m'
else BOLD=; RED=; YEL=; GRN=; RST=; fi
info() { printf '%s==>%s %s\n' "$GRN" "$RST" "$*"; }
warn() { printf '%s!! %s%s\n' "$YEL" "$*" "$RST" >&2; }
die()  { printf '%sxx %s%s\n' "$RED" "$*" "$RST" >&2; exit 1; }

# /dev/tty so prompts work under `curl | bash` (where stdin is the script)
TTY=/dev/tty
ask() {  # ask "Question" "default" -> echoes answer
  local q="$1" def="${2:-}" ans=""
  if [ ! -r "$TTY" ]; then echo "$def"; return; fi
  if [ -n "$def" ]; then printf '%s%s%s [%s]: ' "$BOLD" "$q" "$RST" "$def" >"$TTY"
  else printf '%s%s%s: ' "$BOLD" "$q" "$RST" >"$TTY"; fi
  read -r ans <"$TTY" || true
  echo "${ans:-$def}"
}
confirm() {  # confirm "Question" -> returns 0 if yes
  [ "$DZ_ASSUME_YES" = 1 ] && return 0
  [ -r "$TTY" ] || return 1
  local ans; printf '%s%s%s [y/N]: ' "$BOLD" "$1" "$RST" >"$TTY"
  read -r ans <"$TTY" || true
  case "$ans" in y|Y|yes|YES) return 0;; *) return 1;; esac
}

# ----------------------------------------------------------------------------
# 1. preconditions
# ----------------------------------------------------------------------------
[ "$(uname -s)" = Linux ] || die "This installer supports Linux hosts only (got $(uname -s)). The client needs host networking + kernel tunnels."

case "$(uname -m)" in
  x86_64|amd64) : ;;
  *) die "The doublezero image is published for amd64 only; this host is $(uname -m). Run on an x86_64 Linux box." ;;
esac

# root / sudo: run as a normal user and self-elevate only the privileged steps.
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || die "Need root (for Docker + network capabilities) but sudo is not installed. Re-run as root."
  SUDO="sudo"
fi

# Resolve the *human* user's home so the keypair default points at their files
# whether this is invoked as `... | bash` (self-sudo) or `... | sudo bash` (all root).
if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
  REAL_HOME="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6)"
fi
REAL_HOME="${REAL_HOME:-$HOME}"

# Prime sudo once up front so later privileged commands don't re-prompt mid-run,
# but only ask for a password if one is actually required ('sudo -n true' succeeds
# silently for NOPASSWD or an already-cached timestamp).
if [ -n "$SUDO" ] && ! $SUDO -n true 2>/dev/null; then
  info "Some steps need root; you may be prompted for your password once."
  $SUDO -v || die "Could not obtain sudo. Re-run as root, or configure passwordless sudo."
fi

# ----------------------------------------------------------------------------
# 2. docker present? offer install
# ----------------------------------------------------------------------------
if ! command -v docker >/dev/null 2>&1; then
  warn "Docker is not installed."
  if confirm "Install Docker now via get.docker.com?"; then
    info "Installing Docker..."
    curl -fsSL https://get.docker.com | $SUDO sh
    $SUDO systemctl enable --now docker 2>/dev/null || true
  else
    die "Docker is required. Install it and re-run."
  fi
fi
$SUDO docker info >/dev/null 2>&1 || die "Docker is installed but the daemon isn't reachable. Start it (e.g. 'sudo systemctl start docker') and re-run."

# ----------------------------------------------------------------------------
# 3. host kernel / network prep (host-side; safe to attempt)
# ----------------------------------------------------------------------------
info "Preparing host for GRE tunnels..."
$SUDO modprobe tun 2>/dev/null    || warn "Could not load 'tun' module (may be built-in)."
$SUDO modprobe ip_gre 2>/dev/null || warn "Could not load 'ip_gre' module (will auto-load on tunnel create)."
[ -e /dev/net/tun ] || warn "/dev/net/tun is missing; tunnel creation may fail."

# best-effort firewall hints (don't auto-edit the user's firewall)
if command -v ufw >/dev/null 2>&1 && $SUDO ufw status 2>/dev/null | grep -qi "Status: active"; then
  warn "ufw is active: ensure IP protocol 47 (GRE) and UDP $LIVENESS_UDP_PORT are allowed."
fi
if command -v firewall-cmd >/dev/null 2>&1 && $SUDO firewall-cmd --state 2>/dev/null | grep -qi running; then
  warn "firewalld is running: ensure GRE (protocol 47) and UDP $LIVENESS_UDP_PORT are allowed."
fi

# ----------------------------------------------------------------------------
# 4. cloud detection -> warn about provider-level firewall (script can't fix)
# ----------------------------------------------------------------------------
detect_cloud() {
  local md="http://169.254.169.254"
  # AWS IMDSv2
  local tok
  tok=$(curl -fsS -m 1 -X PUT "$md/latest/api/token" -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' 2>/dev/null || true)
  if [ -n "$tok" ] && curl -fsS -m 1 -H "X-aws-ec2-metadata-token: $tok" "$md/latest/meta-data/instance-id" >/dev/null 2>&1; then echo aws; return; fi
  if curl -fsS -m 1 -H 'Metadata-Flavor: Google' "$md/computeMetadata/v1/instance/id" >/dev/null 2>&1; then echo gcp; return; fi
  if curl -fsS -m 1 -H 'Metadata: true' "$md/metadata/instance?api-version=2021-02-01" >/dev/null 2>&1; then echo azure; return; fi
  echo none
}
CLOUD="$(detect_cloud)"
case "$CLOUD" in
  aws)   warn "AWS detected. GRE will not work until you (in AWS, NOT on this host): 1) allow inbound IP protocol 47 in the Security Group; 2) DISABLE the ENI source/dest check.";;
  gcp)   warn "GCP detected. Add a firewall rule allowing IP protocol 47 (gre) to this instance.";;
  azure) warn "Azure detected. Add an NSG rule allowing IP protocol 47 to this VM.";;
esac

# ----------------------------------------------------------------------------
# 5. inputs: keypair + environment + connect target
# ----------------------------------------------------------------------------
# environment
if [ -z "$DZ_ENV" ]; then DZ_ENV="$(ask 'DoubleZero environment (testnet/devnet/mainnet-beta)' 'mainnet-beta')"; fi
case "$DZ_ENV" in testnet|devnet|mainnet-beta) : ;; *) die "Invalid DZ_ENV '$DZ_ENV'";; esac

# keypair
if [ -z "$DZ_KEYPAIR" ]; then DZ_KEYPAIR="$(ask 'Path to your DoubleZero keypair (id.json)' "$REAL_HOME/.config/doublezero/id.json")"; fi
# expand ~ and relativize to absolute (against the human user's home)
case "$DZ_KEYPAIR" in "~"*) DZ_KEYPAIR="${REAL_HOME}${DZ_KEYPAIR#\~}";; esac
DZ_KEYPAIR="$(realpath -m "$DZ_KEYPAIR" 2>/dev/null || echo "$DZ_KEYPAIR")"
[ -f "$DZ_KEYPAIR" ] || die "No keypair file at: $DZ_KEYPAIR (a wrong path makes Docker mount an empty dir over id.json)."

# SELinux relabel for the bind mount
MNT_OPT=ro
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null)" = Enforcing ]; then MNT_OPT=ro,Z; fi

# ----------------------------------------------------------------------------
# 6. run the container (detached, long-lived daemon)
# ----------------------------------------------------------------------------
info "Pulling $DZ_IMAGE ..."
$SUDO docker pull -q "$DZ_IMAGE" >/dev/null

info "Starting doublezero client (env=$DZ_ENV)..."
$SUDO docker rm -f "$DZ_NAME" >/dev/null 2>&1 || true
$SUDO docker run -d --name "$DZ_NAME" \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN --cap-add NET_RAW \
  --device /dev/net/tun \
  -e DZ_ENV="$DZ_ENV" \
  -v "$DZ_KEYPAIR":"$KEYPAIR_DEST":"$MNT_OPT" \
  "$DZ_IMAGE" >/dev/null

# wait for the daemon socket
info "Waiting for the daemon..."
for _ in $(seq 1 30); do
  $SUDO docker logs "$DZ_NAME" 2>&1 | grep -q "doublezerod ready" && break
  $SUDO docker ps -q --filter "name=^${DZ_NAME}$" | grep -q . || die "Container exited early. Logs: sudo docker logs $DZ_NAME"
  sleep 1
done

# ----------------------------------------------------------------------------
# 7. connect  (TODO: finalize the verb/args + access-pass flow)
# ----------------------------------------------------------------------------
if [ -z "$DZ_CONNECT" ]; then
  DZ_CONNECT="$(ask 'Connect command to run now (Enter to accept, blank-then-Enter to skip)' 'connect multicast')"
fi
if [ -n "$DZ_CONNECT" ]; then
  info "Connecting: doublezero $DZ_CONNECT"
  # Allocate a pseudo-TTY when our stdout is a terminal so the CLI streams its
  # normal output to the screen (without -t, docker exec gives it no TTY and the
  # command's progress/result output is suppressed).
  EXEC_TTY=""; [ -t 1 ] && EXEC_TTY="-t"
  # NOTE: connect requires the host's public IP to have an access pass / allowlisted
  # user onchain for $DZ_ENV. If this errors with an access-pass message, that
  # provisioning step still needs to happen.
  $SUDO docker exec $EXEC_TTY "$DZ_NAME" doublezero $DZ_CONNECT || warn "connect failed (often: no access pass for this IP, or provider firewall/NAT). See notes above."
fi

# ----------------------------------------------------------------------------
# 8. status + management hints
# ----------------------------------------------------------------------------
echo
$SUDO docker exec "$DZ_NAME" doublezero status || true
echo
info "Done. Manage with:"
echo "  sudo docker exec -it $DZ_NAME doublezero status      # tunnel status"
echo "  sudo docker exec -it $DZ_NAME doublezero latency     # device latencies"
echo "  sudo docker logs -f $DZ_NAME                         # daemon logs"
echo "  sudo docker rm -f $DZ_NAME                           # stop & remove"
