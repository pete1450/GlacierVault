#!/usr/bin/env python3
"""Compute Go test coverage excluding files that require live AWS infrastructure.

Usage: coverage_filtered.py <coverprofile> [--threshold N]

Excludes files that can only be tested against real AWS (CloudFront/IAM/STS
API calls, CDK deploys) or the main() entrypoint. Everything else must meet
the threshold.

Prints the filtered coverage percentage and exits 1 if below threshold.
"""
import re
import sys

# Files requiring live AWS — excluded from the coverage total.
EXCLUDE = [
    'internal/cloudfront/provision.go',     # CloudFront API calls
    'internal/cloudfront/manager.go',       # AWS client wiring
    'internal/cloudfront/rotate.go',        # IAM key rotation via AWS
    'internal/provisioning/aws.go',         # STS/IAM via AWS
    'internal/provisioning/provisioner.go',  # CDK deploy, account lookup
    'cmd/server/main.go',                   # main() wiring
]

def is_excluded(path):
    return any(path.endswith(e) for e in EXCLUDE)

def main():
    profile = sys.argv[1]
    threshold = float(sys.argv[3]) if len(sys.argv) > 3 and sys.argv[2] == '--threshold' else 0

    covered = total = 0
    ex_total = 0
    with open(profile) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith('mode:'):
                continue
            parts = line.split()
            if len(parts) != 3:
                continue
            fpath = parts[0].rsplit(':', 1)[0]
            numstmt, count = int(parts[1]), int(parts[2])
            if is_excluded(fpath):
                ex_total += numstmt
                continue
            total += numstmt
            if count > 0:
                covered += numstmt

    pct = 100 * covered / total if total else 0
    print(f"Coverage (excluding {ex_total} AWS-only statements): {pct:.1f}%")
    if threshold and pct < threshold:
        print(f"Below threshold {threshold}%")
        sys.exit(1)
    print(f"Meets threshold {threshold}%")

if __name__ == '__main__':
    main()
