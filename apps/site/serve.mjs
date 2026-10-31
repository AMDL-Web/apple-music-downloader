// The preview server behind build.mjs --serve: the built site on 127.0.0.1,
// each path answered from the output directory.
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';

const TYPES = { '.html': 'text/html; charset=utf-8', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.png': 'image/png' };

export function serve(root, port) {
  http
    .createServer((req, res) => {
      const rel = decodeURIComponent(new URL(req.url, 'http://x').pathname);
      let file = path.join(root, path.normalize(rel));
      if (!file.startsWith(root)) {
        res.writeHead(403).end();
        return;
      }
      if (rel.endsWith('/')) file = path.join(file, 'index.html');
      fs.readFile(file, (err, body) => {
        if (err) {
          res.writeHead(404, { 'Content-Type': 'text/plain' }).end('not found');
          return;
        }
        res.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] ?? 'application/octet-stream' }).end(body);
      });
    })
    .listen(port, '127.0.0.1', () => console.log(`serving ${root} on http://127.0.0.1:${port}/`));
}
