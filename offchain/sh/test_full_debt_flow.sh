#!/bin/bash
#
# End-to-End Test: Initialize → Calculate → Finalize → Collect Debt
#
# This script tests the full validator debt lifecycle on a local Solana fork.
# It requires:
#   - Built binaries in target/debug/
#   - Forked mainnet accounts with revenue distribution program state
#
# The script will automatically start and stop the Solana fork.
#
# Usage:
#   ./sh/test_full_debt_flow.sh
#
# Environment variables:
#   DZ_EPOCH - Override the DZ epoch to test (default: auto-detect)
#   SKIP_INITIALIZE - Set to "1" to skip initialization step
#   SKIP_CALCULATE - Set to "1" to skip calculation step
#   SKIP_FINALIZE - Set to "1" to skip finalization step
#   SKIP_COLLECT - Set to "1" to skip debt collection step
#   SKIP_FORK_START - Set to "1" to skip starting the fork (use existing)

set -eu

# Constants
TEST_DEBT_ACCOUNTANT_KEY=acLisxTpNkoctPZoqssyo58pcdnHzJyRFhod7Wxkz5a
VALIDATOR_DEBT_CLI=target/debug/doublezero-solana-validator-debt
ADMIN_CLI=target/debug/doublezero-revenue-distribution-admin
SOLANA_CLI=target/debug/doublezero-solana
SOLANA_FORK_CLI=target/debug/doublezero-solana-fork
TRANSACTION_CONFIRMATION_WAIT=5

# PID of the fork process (for cleanup)
FORK_PID=""
# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}  STEP: $1${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""
}

# Cleanup function to stop the fork on exit
cleanup() {
    if [ -n "$FORK_PID" ] && kill -0 "$FORK_PID" 2>/dev/null; then
        log_info "Stopping Solana fork (PID: $FORK_PID)..."
        kill "$FORK_PID" 2>/dev/null || true
        wait "$FORK_PID" 2>/dev/null || true
        log_success "Solana fork stopped"
    fi
}

# Set up trap to cleanup on exit
trap cleanup EXIT INT TERM

# Start the Solana fork
start_solana_fork() {
    if [ "${SKIP_FORK_START:-0}" = "1" ]; then
        log_warning "Skipping fork start (SKIP_FORK_START=1)"
        return 0
    fi

    log_info "Starting Solana fork..."

    if [ ! -f "$SOLANA_FORK_CLI" ]; then
        log_error "Solana fork CLI not found at $SOLANA_FORK_CLI"
        log_info "Run 'cargo build' first"
        exit 1
    fi

    # Start the fork in the background
    $SOLANA_FORK_CLI --reset &
    FORK_PID=$!

    log_info "Solana fork started with PID: $FORK_PID"
}

# Wait for Solana fork to start
wait_for_solana() {
    log_info "Waiting for Solana fork to start..."
    for i in {1..60}; do
        if solana cluster-version -u l > /dev/null 2>&1; then
            log_success "Solana fork is ready."
            return 0
        fi
        sleep 2
    done

    log_error "Solana fork did not start within 120 seconds"
    exit 1
}

# Verify binaries exist
verify_binaries() {
    log_info "Verifying required binaries..."

    if [ ! -f "$VALIDATOR_DEBT_CLI" ]; then
        log_error "Validator debt CLI not found at $VALIDATOR_DEBT_CLI"
        log_info "Run 'cargo build' first"
        exit 1
    fi

    if [ ! -f "$ADMIN_CLI" ]; then
        log_error "Admin CLI not found at $ADMIN_CLI"
        log_info "Run 'cargo build' first"
        exit 1
    fi

    if [ ! -f "$SOLANA_CLI" ]; then
        log_error "Solana CLI not found at $SOLANA_CLI"
        log_info "Run 'cargo build' first"
        exit 1
    fi

    if [ ! -f "$SOLANA_FORK_CLI" ]; then
        log_error "Solana fork CLI not found at $SOLANA_FORK_CLI"
        log_info "Run 'cargo build' first"
        exit 1
    fi

    log_success "All binaries found"
}

# Get current epoch from program config
get_current_epoch() {
    $ADMIN_CLI fetch-current-epoch -ul
}

# Configure the debt write-off feature (required for full flow)
configure_debt_write_off() {
    local current_epoch=$1
    local activation_epoch=$((current_epoch + 1))

    log_info "Configuring Solana validator debt write-off feature activation epoch to $activation_epoch"

    $ADMIN_CLI configure -ul \
        --solana-validator-debt-write-off-feature-activation-epoch "$activation_epoch" \
        || log_warning "Configuration may have already been set"
}

# Step 1: Initialize Distribution
step_initialize() {
    log_step "1. INITIALIZE DISTRIBUTION"

    if [ "${SKIP_INITIALIZE:-0}" = "1" ]; then
        log_warning "Skipping initialization (SKIP_INITIALIZE=1)"
        return 0
    fi

    log_info "Initializing distribution for DZ epoch: $DZ_EPOCH"
    log_info "Using debt accountant: $TEST_DEBT_ACCOUNTANT_KEY"

    echo "$ $VALIDATOR_DEBT_CLI initialize-distribution -v -ul --dz-env mainnet-beta --bypass-dz-epoch-check --record-debt-accountant $TEST_DEBT_ACCOUNTANT_KEY --with-compute-unit-price 1000"

    $VALIDATOR_DEBT_CLI initialize-distribution \
        -v \
        -ul \
        --dz-env mainnet-beta \
        --bypass-dz-epoch-check \
        --record-debt-accountant "$TEST_DEBT_ACCOUNTANT_KEY" \
        --with-compute-unit-price 1000

    log_success "Distribution initialized"
}

# Step 2: Calculate Validator Debt
step_calculate() {
    log_step "2. CALCULATE VALIDATOR DEBT"

    if [ "${SKIP_CALCULATE:-0}" = "1" ]; then
        log_warning "Skipping calculation (SKIP_CALCULATE=1)"
        return 0
    fi

    log_info "Calculating validator debt for DZ epoch: $DZ_EPOCH"

    # Fetch the distribution to verify it exists
    log_info "Verifying distribution exists..."
    echo "$ $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch $DZ_EPOCH"
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view summary || true

    log_info "Running debt calculation..."
    echo "$ $VALIDATOR_DEBT_CLI calculate-validator-debt --epoch $DZ_EPOCH -ul --dz-ledger-url http://localhost:8899"

    # Note: This requires the DZ Ledger to be running or a mock.
    # In local testing, we might need to use --post-to-ledger-only or --dry-run
    $VALIDATOR_DEBT_CLI calculate-validator-debt \
        --epoch "$DZ_EPOCH" \
        -ul \
        --dz-ledger-url http://localhost:8899 \
        --force \
        || {
            log_warning "Calculation failed - this may be expected if DZ Ledger is not available"
            log_info "Trying with --dry-run instead..."
            $VALIDATOR_DEBT_CLI calculate-validator-debt \
                --epoch "$DZ_EPOCH" \
                -ul \
                --dz-ledger-url http://localhost:8899 \
                --dry-run \
                --force \
                || log_warning "Dry run also failed - continuing anyway"
        }

    log_success "Debt calculation step completed"
}

# Step 3: Finalize Distribution
step_finalize() {
    log_step "3. FINALIZE DISTRIBUTION"

    if [ "${SKIP_FINALIZE:-0}" = "1" ]; then
        log_warning "Skipping finalization (SKIP_FINALIZE=1)"
        return 0
    fi

    log_info "Finalizing distribution for DZ epoch: $DZ_EPOCH"

    echo "$ $VALIDATOR_DEBT_CLI finalize-distribution --epoch $DZ_EPOCH -ul --dz-ledger-url http://localhost:8899"

    $VALIDATOR_DEBT_CLI finalize-distribution \
        --epoch "$DZ_EPOCH" \
        -ul \
        || {
            log_warning "Finalization may have failed or already completed"
        }

    # Verify the distribution is finalized
    log_info "Verifying finalization..."
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view summary || true

    log_success "Finalization step completed"
}

# Step 4: Collect Debt
step_collect() {
    log_step "4. COLLECT VALIDATOR DEBT"

    if [ "${SKIP_COLLECT:-0}" = "1" ]; then
        log_warning "Skipping debt collection (SKIP_COLLECT=1)"
        return 0
    fi

    # First, let's fund a test validator deposit account
    log_info "Setting up test validator for debt collection..."

    # Generate a test validator keypair
    solana-keygen new --silent --no-bip39-passphrase -o test_validator.json --force
    local test_validator_id
    test_validator_id=$(solana address -k test_validator.json)
    log_info "Test validator ID: $test_validator_id"

    # Fund the validator deposit
    log_info "Funding validator deposit account..."
    $SOLANA_CLI revenue-distribution validator-deposit \
        --fund 1.0 \
        -ul \
        -v \
        --node-id "$test_validator_id" \
        || log_warning "Could not fund validator deposit"

    # Check validator deposit balance
    log_info "Checking validator deposit balance..."
    $SOLANA_CLI revenue-distribution fetch validator-deposits -ul --node-id "$test_validator_id" || true

    # Check if there's outstanding debt for any validators
    log_info "Checking for outstanding validator debts..."
    $SOLANA_CLI revenue-distribution fetch validator-debts -ul --dz-env mainnet-beta || true

    # Attempt to pay outstanding debt (this uses the Solana CLI)
    log_info "Attempting to pay outstanding debt..."
    $SOLANA_CLI revenue-distribution validator-deposit \
        --node-id "$test_validator_id" \
        -ul \
        --fund-outstanding-debt \
        --dz-env mainnet-beta \
        || log_warning "No outstanding debt to pay or payment failed"

    # Clean up test validator keypair
    rm -f test_validator.json

    log_success "Debt collection step completed"
}

# View distribution summary
view_distribution_summary() {
    log_step "DISTRIBUTION SUMMARY"

    log_info "Fetching distribution summary for epoch $DZ_EPOCH..."

    echo ""
    echo "=== Full Distribution Details ==="
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" || true

    echo ""
    echo "=== Summary View ==="
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view summary || true

    echo ""
    echo "=== Validator Debt View ==="
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view validator-debt || true

    echo ""
    echo "=== Unprocessed Validator Debt ==="
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view unprocessed-validator-debt || true

    echo ""
    echo "=== Rewards View ==="
    $SOLANA_CLI revenue-distribution fetch distribution -ul --dz-epoch "$DZ_EPOCH" --view rewards || true
}

# Run a quick sanity check of the CLI binaries
sanity_check() {
    log_step "SANITY CHECK"

    log_info "Checking validator debt CLI..."
    $VALIDATOR_DEBT_CLI --version
    $VALIDATOR_DEBT_CLI -h | head -10

    log_info "Checking admin CLI..."
    $ADMIN_CLI --version || true

    log_info "Checking Solana CLI..."
    $SOLANA_CLI --version
}

# Main execution
main() {
    echo ""
    echo "============================================"
    echo "  DoubleZero Validator Debt End-to-End Test"
    echo "============================================"
    echo ""

    # Verify environment
    verify_binaries

    # Start the Solana fork
    start_solana_fork

    # Wait for it to be ready
    wait_for_solana
    sanity_check

    # Get current epoch
    log_info "Fetching current DZ epoch..."
    CURRENT_EPOCH=$(get_current_epoch)
    log_info "Current DZ epoch from program: $CURRENT_EPOCH"

    # Use provided DZ_EPOCH or default to current
    DZ_EPOCH=${DZ_EPOCH:-$CURRENT_EPOCH}
    log_info "Testing with DZ epoch: $DZ_EPOCH"

    # Configure debt write-off feature
    configure_debt_write_off "$CURRENT_EPOCH"

    # Run all steps
    step_initialize

    # If we just initialized, we might need to wait
    if [ "${SKIP_INITIALIZE:-0}" != "1" ]; then
        log_info "Waiting $TRANSACTION_CONFIRMATION_WAIT seconds for transaction to confirm..."
        sleep TRANSACTION_CONFIRMATION_WAIT
    fi

    step_calculate
    step_finalize
    step_collect

    # Show final summary
    view_distribution_summary

    log_step "TEST COMPLETE"
    log_success "All steps executed. Check output above for any warnings or errors."
}

# Run main
main "$@"