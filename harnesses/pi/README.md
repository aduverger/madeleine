# @aduverger/pi-madeleine

Pi adapter for [Madeleine](https://github.com/aduverger/madeleine), a local-first
memory layer that attaches historical coding-agent context to repository paths.

## Install

Install the latest matching Madeleine application and Pi package:

```sh
go install github.com/aduverger/madeleine/cmd/madeleine@latest
pi install npm:@aduverger/pi-madeleine
```

The `madeleine` binary must be on `PATH`. Set `MADELEINE_BIN` to use another
binary location and `MADELEINE_HOME` to override the local data directory.

See the [Madeleine README](https://github.com/aduverger/madeleine#readme) for
usage, recovery behavior, storage, and privacy details.

## License

Apache-2.0
