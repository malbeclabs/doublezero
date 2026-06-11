#!/usr/bin/env bash
#
# DoubleZero Edge installer (devnet)
# ------------------------------------
# Served from https://get.doublezero.xyz/install-devnet and run as:
#
#     curl -fsSL https://get.doublezero.xyz/install-devnet | bash
#
# It checks for Docker (offering to install it), preps the host for GRE, loads the
# access secret, runs the thin doublezero client container
# (ghcr.io/malbeclabs/doublezero-devnet:latest), and runs `doublezero connect multicast`.
#
# Attendantless: the only input is the access secret. Provide it via DZ_SECRET to
# run with no prompts at all; otherwise you're prompted once. Everything else has
# a default.
#
# Env vars:
#   DZ_SECRET=<DZ_token|path> base64 keypair token (always prefixed with 'DZ_')
#                             OR a path to a keypair file. If set, runs non-interactively.
#   DZ_ENV=testnet|devnet|mainnet-beta   default: devnet
#   DZ_IMAGE=ghcr.io/malbeclabs/doublezero-devnet:latest
#   DZ_NAME=doublezero                   container name
#   DZ_ASSUME_YES=1                      skip confirmation prompts (e.g. Docker install)
#   DZ_GHCR_TOKEN=<token>   ghcr token with read:packages (required: the devnet
#                           image is private). DZ_GHCR_USER defaults to the token owner.
#   DZ_GHCR_USER=<user>     optional; ghcr username for the login (default: malbeclabs)
#
# A DZ_-token-derived keypair is injected straight into the container and is never
# written to the host disk; a keypair supplied as a file path is bind-mounted
# read-only. Either way the plaintext key is not printed.
#
# NOTE: connecting requires the host's public IP to have an access pass / allowlisted
# user onchain for the chosen environment. That provisioning is a separate step; if
# `connect` reports an access-pass error, the rest of the setup is still in place.

set -euo pipefail

# ----------------------------------------------------------------------------
# config / defaults
# ----------------------------------------------------------------------------
DZ_IMAGE="${DZ_IMAGE:-ghcr.io/malbeclabs/doublezero-devnet:latest}"
DZ_NAME="${DZ_NAME:-doublezero}"
DZ_ENV="${DZ_ENV:-devnet}"
DZ_SECRET="${DZ_SECRET:-}"
DZ_ASSUME_YES="${DZ_ASSUME_YES:-0}"
DZ_GHCR_TOKEN="${DZ_GHCR_TOKEN:-}"
DZ_GHCR_USER="${DZ_GHCR_USER:-malbeclabs}"
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
info "Preparing host for DoubleZero Edge Connect"
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
# 5. input: the access secret (the only thing we ask for)
# ----------------------------------------------------------------------------
# Environment: default devnet, override via DZ_ENV; never prompted.
case "$DZ_ENV" in testnet|devnet|mainnet-beta) : ;; *) die "Invalid DZ_ENV '$DZ_ENV' (testnet|devnet|mainnet-beta)";; esac

# The secret is either a base64 keypair token (always prefixed with 'DZ_') or a
# path to an existing keypair file. Provide via DZ_SECRET (no prompt) or once at
# the prompt.
SECRET="$DZ_SECRET"
if [ -z "$SECRET" ]; then
  SECRET="$(ask 'Access secret (DZ_-prefixed token, or path to a keypair file)' '')"
fi
[ -n "$SECRET" ] || die "No secret provided. Set DZ_SECRET or supply one when prompted."

case "$SECRET" in
  DZ_*)
    # 'DZ_' + base64url(raw 64 keypair bytes). Restore the base64 alphabet and
    # padding, decode to raw bytes, and build the Solana keypair JSON array in
    # memory. It is injected into the container after start and is NEVER written
    # to the host disk.
    b64="$(printf '%s' "${SECRET#DZ_}" | tr '_-' '/+')"
    case $(( ${#b64} % 4 )) in
      2) b64="${b64}==" ;;
      3) b64="${b64}=" ;;
      1) die "Invalid DZ_ token (bad base64url length)." ;;
    esac
    # raw bytes -> single-line "[b1,b2,...]" (collapse od's multi-line output)
    KEY_JSON="[$(printf '%s' "$b64" | base64 -d 2>/dev/null | od -An -v -t u1 | tr -s ' \n' ' ' | sed 's/^ //; s/ $//; s/ /,/g')]"
    n=$(printf '%s' "$KEY_JSON" | tr ',' '\n' | grep -c '[0-9]')
    [ "$n" -eq 64 ] || die "DZ_ token did not decode to a 64-byte keypair (got $n)."
    KEY_SRC=token
    info "Decoded token to a $n-byte keypair (held in memory; not written to host disk)."
    ;;
  *)
    # otherwise it's a path to an existing keypair file (bind-mounted read-only)
    case "$SECRET" in "~"*) SECRET_PATH="${REAL_HOME}${SECRET#\~}";; *) SECRET_PATH="$SECRET";; esac
    [ -f "$SECRET_PATH" ] || die "Secret is not a DZ_-prefixed token, and no keypair file exists at: $SECRET"
    KEYFILE="$(realpath -m "$SECRET_PATH")"
    KEY_SRC=file
    info "Using keypair file: $KEYFILE"
    ;;
esac

# SELinux relabel for the bind mount
MNT_OPT=ro
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null)" = Enforcing ]; then MNT_OPT=ro,Z; fi

# ----------------------------------------------------------------------------
# 6. run the container (detached, long-lived daemon)
# ----------------------------------------------------------------------------
# The devnet image is private; authenticate to ghcr before pulling.
if [ -z "$DZ_GHCR_TOKEN" ]; then
  die "The devnet image is private. Set DZ_GHCR_TOKEN (a ghcr token with read:packages) and re-run."
fi
info "Logging in to ghcr.io as $DZ_GHCR_USER ..."
printf '%s' "$DZ_GHCR_TOKEN" | $SUDO docker login ghcr.io -u "$DZ_GHCR_USER" --password-stdin >/dev/null \
  || die "ghcr login failed. Check DZ_GHCR_TOKEN/DZ_GHCR_USER."

info "Pulling $DZ_IMAGE ..."
$SUDO docker pull -q "$DZ_IMAGE" >/dev/null

info "Starting doublezero client (env=$DZ_ENV)..."
$SUDO docker rm -f "$DZ_NAME" >/dev/null 2>&1 || true
# Bind-mount the keypair only when the secret was a file; a token-derived key is
# injected into the container after it starts (so it never touches the host disk).
mount_args=()
[ "$KEY_SRC" = file ] && mount_args=(-v "$KEYFILE":"$KEYPAIR_DEST":"$MNT_OPT")
$SUDO docker run -d --name "$DZ_NAME" \
  --restart unless-stopped \
  --network host \
  --cap-add NET_ADMIN --cap-add NET_RAW \
  --device /dev/net/tun \
  -e DZ_ENV="$DZ_ENV" \
  "${mount_args[@]}" \
  "$DZ_IMAGE" >/dev/null

# wait for the daemon socket
info "Waiting for the daemon..."
for _ in $(seq 1 30); do
  $SUDO docker logs "$DZ_NAME" 2>&1 | grep -q "doublezerod ready" && break
  $SUDO docker ps -q --filter "name=^${DZ_NAME}$" | grep -q . || die "Container exited early. Logs: sudo docker logs $DZ_NAME"
  sleep 1
done

# Deliver a token-derived key by piping it straight into the container's
# filesystem, so the plaintext keypair lives only in the container (and is gone
# when the container is removed). A file secret is already bind-mounted above.
if [ "$KEY_SRC" = token ]; then
  printf '%s' "$KEY_JSON" | $SUDO docker exec -i "$DZ_NAME" \
    sh -c 'umask 077; mkdir -p /root/.config/doublezero && cat > /root/.config/doublezero/id.json'
  info "Installed keypair into the container (not stored on the host)."
fi

# ----------------------------------------------------------------------------
# 7. connect (always `doublezero connect multicast`)
# ----------------------------------------------------------------------------
info "Connecting: doublezero connect multicast"
# Allocate a pseudo-TTY when our stdout is a terminal so the CLI streams its
# normal output to the screen (without -t, docker exec gives it no TTY and the
# command's output is suppressed).
EXEC_TTY=""; [ -t 1 ] && EXEC_TTY="-t"
# NOTE: connect requires the host's public IP to have an access pass / allowlisted
# user onchain for $DZ_ENV. If this errors with an access-pass message, that
# provisioning step still needs to happen.
$SUDO docker exec $EXEC_TTY "$DZ_NAME" doublezero connect multicast || warn "connect failed (often: no access pass for this IP, or provider firewall/NAT). See notes above."

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
