# GitHub Action Client

`client/` will contain the GitHub Action packaging layer. It is not a web client.

The Action will be implemented only after `server/` can produce a versioned, checksum-verified Linux binary. Until then, this directory intentionally contains no `action.yml` and cannot be installed or run.
