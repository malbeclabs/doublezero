#!/bin/bash
#
# Parallel eval test runner with retry support
#
# Usage: ./scripts/run-evals.sh [options]
#   -r, --retries N     Number of retries per test (default: 0)
#   -p, --parallel N    Max parallel tests (default: 8)
#   -t, --timeout N     Timeout per test in minutes (default: 5)
#   -f, --filter REGEX  Only run tests matching regex (default: Anthropic)
#   -s, --short         Run in short mode (code validation only)
#   --show-failures     Print all failure logs to stdout at the end
#   -h, --help          Show help

set -euo pipefail

# Defaults
RETRIES=0
MAX_PARALLEL=8
TIMEOUT_MINS=5
FILTER="Anthropic"
SHORT_MODE=""
SHOW_FAILURES=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--retries)
            RETRIES="$2"
            shift 2
            ;;
        -p|--parallel)
            MAX_PARALLEL="$2"
            shift 2
            ;;
        -t|--timeout)
            TIMEOUT_MINS="$2"
            shift 2
            ;;
        -f|--filter)
            FILTER="$2"
            shift 2
            ;;
        -s|--short)
            SHORT_MODE="-short"
            shift
            ;;
        --show-failures)
            SHOW_FAILURES=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo "  -r, --retries N     Number of retries per test (default: 0)"
            echo "  -p, --parallel N    Max parallel tests (default: 8)"
            echo "  -t, --timeout N     Timeout per test in minutes (default: 5)"
            echo "  -f, --filter REGEX  Only run tests matching regex (default: Anthropic)"
            echo "  -s, --short         Run in short mode (code validation only)"
            echo "  --show-failures     Print all failure logs to stdout at the end"
            echo "  -h, --help          Show help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Get lake root directory (parent of scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAKE_ROOT="$(dirname "$SCRIPT_DIR")"
EVALS_DIR="$LAKE_ROOT/agent/evals"

# Change to evals directory for running tests
cd "$EVALS_DIR"

# Find timeout command (gtimeout on macOS via coreutils)
TIMEOUT_CMD=""
if command -v timeout &>/dev/null; then
    TIMEOUT_CMD="timeout"
elif command -v gtimeout &>/dev/null; then
    TIMEOUT_CMD="gtimeout"
fi

# Create timestamped output directory
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
OUTPUT_DIR="$LAKE_ROOT/eval-runs/$TIMESTAMP"
mkdir -p "$OUTPUT_DIR"

# Output files
SUCCESSES_FILE="$OUTPUT_DIR/successes.log"
FAILURES_FILE="$OUTPUT_DIR/failures.log"
FLAKY_FILE="$OUTPUT_DIR/flaky.log"
SUMMARY_FILE="$OUTPUT_DIR/summary.log"

# Lock directory for portable mutex
LOCK_DIR="$OUTPUT_DIR/.locks"
mkdir -p "$LOCK_DIR"

# Initialize files
touch "$SUCCESSES_FILE" "$FAILURES_FILE" "$FLAKY_FILE"

echo "=== Eval Test Runner ===" | tee "$SUMMARY_FILE"
echo "Output directory: $OUTPUT_DIR" | tee -a "$SUMMARY_FILE"
echo "Retries: $RETRIES" | tee -a "$SUMMARY_FILE"
echo "Max parallel: $MAX_PARALLEL" | tee -a "$SUMMARY_FILE"
echo "Timeout: ${TIMEOUT_MINS}m" | tee -a "$SUMMARY_FILE"
if [[ -n "$FILTER" ]]; then
    echo "Filter: $FILTER" | tee -a "$SUMMARY_FILE"
fi
if [[ -n "$SHORT_MODE" ]]; then
    echo "Mode: short (code validation only)" | tee -a "$SUMMARY_FILE"
fi
echo "" | tee -a "$SUMMARY_FILE"

# Get list of test functions
echo "Discovering tests..."
TEST_LIST=$(go test -tags=evals -list='Test.*' ./... 2>/dev/null | grep -E '^Test' | sort | uniq)

# Apply filter if specified
if [[ -n "$FILTER" ]]; then
    TEST_LIST=$(echo "$TEST_LIST" | grep -E "$FILTER" || true)
fi

# Convert to array (portable method for older bash)
TESTS=()
while IFS= read -r line; do
    [[ -n "$line" ]] && TESTS+=("$line")
done <<< "$TEST_LIST"
TOTAL_TESTS=${#TESTS[@]}

if [[ $TOTAL_TESTS -eq 0 ]]; then
    echo "No tests found matching criteria" | tee -a "$SUMMARY_FILE"
    exit 0
fi

echo "Found $TOTAL_TESTS tests" | tee -a "$SUMMARY_FILE"
echo "" | tee -a "$SUMMARY_FILE"

# Counters (using temp files for atomic updates from subshells)
PASS_COUNT_FILE="$OUTPUT_DIR/.pass_count"
FAIL_COUNT_FILE="$OUTPUT_DIR/.fail_count"
FLAKY_COUNT_FILE="$OUTPUT_DIR/.flaky_count"
echo "0" > "$PASS_COUNT_FILE"
echo "0" > "$FAIL_COUNT_FILE"
echo "0" > "$FLAKY_COUNT_FILE"

# Portable mutex using mkdir (atomic on POSIX systems)
acquire_lock() {
    local lock_name="$1"
    local lock_path="$LOCK_DIR/$lock_name"
    while ! mkdir "$lock_path" 2>/dev/null; do
        sleep 0.01
    done
}

release_lock() {
    local lock_name="$1"
    local lock_path="$LOCK_DIR/$lock_name"
    rmdir "$lock_path" 2>/dev/null || true
}

# Function to run a single test with retries
run_test() {
    local test_name="$1"
    local test_output_file="$OUTPUT_DIR/${test_name}.log"
    local attempt=0
    local max_attempts=$((RETRIES + 1))
    local passed=false
    local all_output=""

    # Change to evals directory
    cd "$EVALS_DIR"

    while [[ $attempt -lt $max_attempts ]]; do
        attempt=$((attempt + 1))

        if [[ $attempt -gt 1 ]]; then
            all_output+="
=== RETRY $attempt/$max_attempts ===
"
        fi

        # Run the test with optional timeout
        local start_time=$(date +%s)
        local output
        local exit_code=0

        if [[ -n "$TIMEOUT_CMD" ]]; then
            output=$($TIMEOUT_CMD "${TIMEOUT_MINS}m" go test -tags=evals -v -count=1 $SHORT_MODE -run "^${test_name}$" ./... 2>&1) || exit_code=$?
        else
            output=$(go test -tags=evals -v -count=1 $SHORT_MODE -run "^${test_name}$" ./... 2>&1) || exit_code=$?
        fi

        local end_time=$(date +%s)
        local duration=$((end_time - start_time))

        all_output+="$output
--- Duration: ${duration}s ---
"

        if [[ $exit_code -eq 0 ]]; then
            passed=true
            break
        elif [[ $exit_code -eq 124 ]]; then
            all_output+="--- TIMEOUT after ${TIMEOUT_MINS}m ---
"
        fi

        # Show retry message if more attempts remain
        if [[ $attempt -lt $max_attempts ]]; then
            echo "FAIL: $test_name (retry $((attempt + 1))/$max_attempts)..."
        fi
    done

    # Write full output to test-specific file
    echo "$all_output" > "$test_output_file"

    # Update counters and append to appropriate log file (with locking)
    if [[ "$passed" == "true" ]]; then
        acquire_lock "pass_count"
        count=$(cat "$PASS_COUNT_FILE")
        echo $((count + 1)) > "$PASS_COUNT_FILE"
        release_lock "pass_count"

        # Note if retries were needed
        local pass_suffix=""
        local was_flaky=false
        if [[ $attempt -gt 1 ]]; then
            pass_suffix=" (after $((attempt - 1)) retry)"
            [[ $attempt -gt 2 ]] && pass_suffix=" (after $((attempt - 1)) retries)"
            was_flaky=true
        fi

        acquire_lock "successes"
        {
            echo "========================================"
            echo "PASS: $test_name$pass_suffix"
            echo "========================================"
            echo "$all_output"
            echo ""
        } >> "$SUCCESSES_FILE"
        release_lock "successes"

        # Also log to flaky file if test required retries
        if [[ "$was_flaky" == "true" ]]; then
            acquire_lock "flaky_count"
            count=$(cat "$FLAKY_COUNT_FILE")
            echo $((count + 1)) > "$FLAKY_COUNT_FILE"
            release_lock "flaky_count"

            acquire_lock "flaky"
            {
                echo "========================================"
                echo "FLAKY: $test_name$pass_suffix"
                echo "========================================"
                echo "$all_output"
                echo ""
            } >> "$FLAKY_FILE"
            release_lock "flaky"
        fi

        echo "PASS: $test_name$pass_suffix"
    else
        acquire_lock "fail_count"
        count=$(cat "$FAIL_COUNT_FILE")
        echo $((count + 1)) > "$FAIL_COUNT_FILE"
        release_lock "fail_count"

        acquire_lock "failures"
        {
            echo "========================================"
            echo "FAIL: $test_name (after $max_attempts attempts)"
            echo "========================================"
            echo "$all_output"
            echo ""
        } >> "$FAILURES_FILE"
        release_lock "failures"

        echo "FAIL: $test_name"
    fi
}

export -f run_test acquire_lock release_lock
export OUTPUT_DIR RETRIES TIMEOUT_MINS SHORT_MODE TIMEOUT_CMD EVALS_DIR
export PASS_COUNT_FILE FAIL_COUNT_FILE FLAKY_COUNT_FILE
export SUCCESSES_FILE FAILURES_FILE FLAKY_FILE
export LOCK_DIR

# Run tests in parallel using xargs
echo "Running tests..."
echo ""
printf '%s\n' "${TESTS[@]}" | xargs -P "$MAX_PARALLEL" -I {} bash -c 'run_test "$@"' _ {}

# Wait for all background jobs
wait

# Read final counts
PASSED=$(cat "$PASS_COUNT_FILE")
FAILED=$(cat "$FAIL_COUNT_FILE")
FLAKY=$(cat "$FLAKY_COUNT_FILE")

# Cleanup lock directory
rm -rf "$LOCK_DIR"

# Print summary
echo "" | tee -a "$SUMMARY_FILE"
echo "=== Summary ===" | tee -a "$SUMMARY_FILE"
echo "Passed: $PASSED/$TOTAL_TESTS" | tee -a "$SUMMARY_FILE"
echo "Failed: $FAILED/$TOTAL_TESTS" | tee -a "$SUMMARY_FILE"
if [[ $FLAKY -gt 0 ]]; then
    echo "Flaky:  $FLAKY (passed on retry)" | tee -a "$SUMMARY_FILE"
fi

# Parse and summarize token usage from all test logs
if ls "$OUTPUT_DIR"/Test*.log &>/dev/null; then
    echo "" | tee -a "$SUMMARY_FILE"
    echo "=== Token Usage ===" | tee -a "$SUMMARY_FILE"

    # Extract token data: phase, inputTokens, outputTokens, cacheCreation, cacheRead
    TOKEN_DATA=$(grep -h "Anthropic API call completed" "$OUTPUT_DIR"/Test*.log 2>/dev/null | \
        sed -n 's/.*phase=\([^ ]*\).*inputTokens=\([0-9]*\).*outputTokens=\([0-9]*\).*cacheCreationInputTokens=\([0-9]*\).*cacheReadInputTokens=\([0-9]*\).*/\1 \2 \3 \4 \5/p' || true)

    # Count tests that had API calls (for per-question cost calculation)
    TESTS_WITH_CALLS=0
    for logfile in "$OUTPUT_DIR"/Test*.log; do
        if [[ -f "$logfile" ]] && grep -q "Anthropic API call completed" "$logfile" 2>/dev/null; then
            TESTS_WITH_CALLS=$((TESTS_WITH_CALLS + 1))
        fi
    done

    if [[ -n "$TOKEN_DATA" ]]; then
        # Calculate stats using awk
        # Haiku 4.5 pricing: $1/M input, $5/M output, $1.25/M cache write, $0.10/M cache read
        echo "$TOKEN_DATA" | awk -v tests_with_calls="$TESTS_WITH_CALLS" '
        {
            phase = $1
            input = $2
            output = $3
            cache_create = $4
            cache_read = $5

            count[phase]++
            input_sum[phase] += input
            output_sum[phase] += output
            cache_create_sum[phase] += cache_create
            cache_read_sum[phase] += cache_read

            if (input > input_max[phase]) input_max[phase] = input
            if (output > output_max[phase]) output_max[phase] = output

            grand_input += input
            grand_output += output
            grand_cache_create += cache_create
            grand_cache_read += cache_read
            grand_count++
        }
        END {
            for (phase in count) {
                printf "  %s: %d calls, %d input, %d output (%d total)\n",
                    phase, count[phase], input_sum[phase], output_sum[phase], input_sum[phase] + output_sum[phase]
                printf "         avg: %d input, %d output | max: %d input, %d output\n",
                    input_sum[phase]/count[phase], output_sum[phase]/count[phase],
                    input_max[phase], output_max[phase]
            }
            printf "  ---\n"
            printf "  total: %d calls, %d input, %d output (%d total tokens)\n",
                grand_count, grand_input, grand_output, grand_input + grand_output

            # Cost calculation (Haiku 4.5 pricing)
            input_cost = grand_input / 1000000.0
            output_cost = grand_output * 5 / 1000000.0
            cache_write_cost = grand_cache_create * 1.25 / 1000000.0
            cache_read_cost = grand_cache_read * 0.10 / 1000000.0
            total_cost = input_cost + output_cost + cache_write_cost + cache_read_cost

            printf "\n=== Estimated Cost (Haiku 4.5) ===\n"
            printf "  input:       $%.4f (%d tokens @ $1/M)\n", input_cost, grand_input
            printf "  output:      $%.4f (%d tokens @ $5/M)\n", output_cost, grand_output
            if (grand_cache_create > 0) {
                printf "  cache write: $%.4f (%d tokens @ $1.25/M)\n", cache_write_cost, grand_cache_create
            }
            if (grand_cache_read > 0) {
                printf "  cache read:  $%.4f (%d tokens @ $0.10/M)\n", cache_read_cost, grand_cache_read
            }
            printf "  ---\n"
            printf "  total: $%.2f\n", total_cost
            if (tests_with_calls > 0) {
                printf "  per question: $%.4f (avg of %d questions)\n", total_cost / tests_with_calls, tests_with_calls
            }
        }
        ' | tee -a "$SUMMARY_FILE"

        # Calculate LLM calls per test/analysis
        echo "" | tee -a "$SUMMARY_FILE"
        echo "=== LLM Calls Per Analysis ===" | tee -a "$SUMMARY_FILE"

        # Count agent calls per test and compute stats
        calls_data=""
        for logfile in "$OUTPUT_DIR"/Test*.log; do
            if [[ -f "$logfile" ]]; then
                # Count agent phase calls (excluding eval phase which is the test harness)
                agent_calls=$(grep -c "phase=agent" "$logfile" 2>/dev/null || echo "0")
                # Extract just the first number (handles grep -c edge cases)
                agent_calls=$(echo "$agent_calls" | head -1 | tr -cd '0-9')
                agent_calls=${agent_calls:-0}
                if [[ "$agent_calls" -gt 0 ]]; then
                    calls_data+="$agent_calls "
                fi
            fi
        done

        if [[ -n "$calls_data" ]]; then
            echo "$calls_data" | tr ' ' '\n' | grep -v '^$' | awk '
            BEGIN { min = 999999 }
            {
                sum += $1
                count++
                if ($1 < min) min = $1
                if ($1 > max) max = $1
            }
            END {
                if (count > 0) {
                    printf "  min: %d | avg: %.1f | max: %d calls per analysis (%d tests)\n", min, sum/count, max, count
                }
            }
            ' | tee -a "$SUMMARY_FILE"
        fi

        # Calculate SQL errors per test
        echo "" | tee -a "$SUMMARY_FILE"
        echo "=== SQL Errors Per Analysis ===" | tee -a "$SUMMARY_FILE"

        errors_data=""
        total_errors=0
        tests_with_errors=0
        for logfile in "$OUTPUT_DIR"/Test*.log; do
            if [[ -f "$logfile" ]]; then
                # Count "workflow: query returned error" messages
                sql_errors=$(grep -c "workflow: query returned error" "$logfile" 2>/dev/null || echo "0")
                # Extract just the first number (handles grep -c edge cases)
                sql_errors=$(echo "$sql_errors" | head -1 | tr -cd '0-9')
                sql_errors=${sql_errors:-0}
                errors_data+="$sql_errors "
                total_errors=$((total_errors + sql_errors))
                if [[ "$sql_errors" -gt 0 ]]; then
                    tests_with_errors=$((tests_with_errors + 1))
                fi
            fi
        done

        if [[ -n "$errors_data" ]]; then
            test_count=$(echo "$errors_data" | tr ' ' '\n' | grep -v '^$' | wc -l | tr -d ' ')
            if [[ $total_errors -eq 0 ]]; then
                echo "  no SQL errors across $test_count tests" | tee -a "$SUMMARY_FILE"
            else
                echo "$errors_data" | tr ' ' '\n' | grep -v '^$' | awk -v total="$total_errors" -v with_errors="$tests_with_errors" '
                BEGIN { min = 999999 }
                {
                    sum += $1
                    count++
                    if ($1 < min) min = $1
                    if ($1 > max) max = $1
                }
                END {
                    if (count > 0) {
                        printf "  total: %d errors across %d/%d tests | max: %d per test\n", total, with_errors, count, max
                    }
                }
                ' | tee -a "$SUMMARY_FILE"
            fi
        fi
    else
        echo "  (no token data - short mode or no API calls)" | tee -a "$SUMMARY_FILE"
    fi
fi

echo "" | tee -a "$SUMMARY_FILE"
echo "Results saved to:" | tee -a "$SUMMARY_FILE"
echo "  Successes: $SUCCESSES_FILE" | tee -a "$SUMMARY_FILE"
echo "  Failures:  $FAILURES_FILE" | tee -a "$SUMMARY_FILE"
if [[ $FLAKY -gt 0 ]]; then
    echo "  Flaky:     $FLAKY_FILE" | tee -a "$SUMMARY_FILE"
fi
echo "  Summary:   $SUMMARY_FILE" | tee -a "$SUMMARY_FILE"
echo "  Per-test:  $OUTPUT_DIR/<test_name>.log" | tee -a "$SUMMARY_FILE"

# List flaky tests (before failures so failures are more visible at the end)
if [[ $FLAKY -gt 0 ]]; then
    echo "" | tee -a "$SUMMARY_FILE"
    echo "Flaky tests:" | tee -a "$SUMMARY_FILE"
    grep -E "^FLAKY:" "$FLAKY_FILE" | sed 's/^FLAKY: /  - /' | head -50 | tee -a "$SUMMARY_FILE"
fi

# List failed tests
if [[ $FAILED -gt 0 ]]; then
    echo "" | tee -a "$SUMMARY_FILE"
    echo "Failed tests:" | tee -a "$SUMMARY_FILE"
    grep -E "^FAIL:" "$FAILURES_FILE" | sed 's/^FAIL: /  - /' | head -50 | tee -a "$SUMMARY_FILE"
fi

# Show full failure logs if requested
if [[ $FAILED -gt 0 ]] && [[ "$SHOW_FAILURES" == "true" ]]; then
    echo ""
    echo "================================================================================"
    echo "                              FAILURE LOGS"
    echo "================================================================================"
    echo ""
    cat "$FAILURES_FILE"
    echo ""
    echo "================================================================================"
    echo "                           FAILURE SUMMARY"
    echo "================================================================================"
    echo ""
    echo "Failed $FAILED of $TOTAL_TESTS tests:"
    grep -E "^FAIL:" "$FAILURES_FILE" | sed 's/^FAIL: /  - /'
    echo ""
fi

# Exit with failure if any tests failed
if [[ $FAILED -gt 0 ]]; then
    exit 1
fi
