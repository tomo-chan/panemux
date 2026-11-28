# agmsg v1.8.2 fixtures

Captured at verified commit c4934bcd37e34fdee28691cf143f7188e4203eae.

capture.sh <source-checkout> <commit-sha> installs into a throwaway HOME, joins a team, sends four messages and captures api.sh JSONL. It requires sqlite3 and Python 3. VERSION.txt and COMMIT.txt record provenance. Project paths are sanitized to /tmp/sample-project. The tests derive IDs and timestamps from this capture; the original v1.2.0 directory retains the historical ID-order regression.
