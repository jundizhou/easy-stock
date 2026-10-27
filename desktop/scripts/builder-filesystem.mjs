import fs from 'node:fs';
import gracefulFs from 'graceful-fs';

// osx-sign scans every bundled file concurrently through isbinaryfile, which
// captures fs.open on import. Queue/retry exhausted descriptors before loading
// electron-builder so a full Hermes source tree can still be signed completely.
gracefulFs.gracefulify(fs);
