#!/bin/bash
# Soak-fuzz the SFZ text parser: go test -run=NONE -fuzz=FuzzSFZParse -fuzztime=SECONDS ./pkg/instruments/sampler/
# Usage: ./scripts/soak_fuzz.sh [SECONDS]   (default: 600)
# Log:   /tmp/soak_fuzz_YYYYMMDD_HHMMSS.log
# Prints total execs and crasher count at the end; exits non-zero on crashers.
# Nightly example (not installed, add manually if wanted):
#   0 3 * * * /path/to/resonata/scripts/soak_fuzz.sh
set -u
cd "$(dirname "$0")/.."

SECS="${1:-600}"
LOG="/tmp/soak_fuzz_$(date +%Y%m%d_%H%M%S).log"
echo "soak fuzz FuzzSFZParse for ${SECS}s, log: $LOG" | tee "$LOG"

go test -run=NONE -fuzz=FuzzSFZParse -fuzztime="${SECS}s" ./pkg/instruments/sampler/ >>"$LOG" 2>&1
FUZZ_EXIT=$?

EXECS=$(grep -oE "execs: [0-9]+" "$LOG" | tail -n 1)
CRASHERS=$(grep -cE "failing input written|^--- FAIL|^panic:" "$LOG" || true)
echo "execs: ${EXECS:-execs: 0}, crashers: $CRASHERS, fuzz_exit: $FUZZ_EXIT" | tee -a "$LOG"

if [ "$FUZZ_EXIT" -ne 0 ] || [ "$CRASHERS" -ne 0 ]; then
  echo "SOAK RESULT: FAIL ($CRASHERS crashers, see $LOG)"
  exit 1
fi
echo "SOAK RESULT: PASS (see $LOG)"
