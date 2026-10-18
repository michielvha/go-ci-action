# Repository instructions

Follow the relevant routes in
https://github.com/vhco-pro/docs/blob/e4d58d2cd69bd42f1efacf91dfdd3f01a24e134a/AGENTS.md.

This composite action owns Go CI checks, not checkout, tagging, signing, or
publication. Shared delivery guidance belongs in `vhco-pro/docs`.

Run `bun test`. The integration workflow runs the composite against
`tests/fixture` before running the command-failure tests.
