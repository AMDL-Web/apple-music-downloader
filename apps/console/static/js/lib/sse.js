// Incremental text/event-stream parser (WHATWG "server-sent events",
// section "Parsing an event stream"). Pure: feed it decoded text chunks.

/** @typedef {{type: string, data: string, id: string|null}} SSEEvent */

export class SSEParser {
  constructor() {
    this.buffer = '';
    this.data = [];
    this.type = '';
    this.id = null;
    this.hasId = false;
    this.pendingCR = false;
  }

  /**
   * Parses one chunk and returns the events it completed. Lines may end in
   * LF, CRLF or a lone CR, and a CRLF pair may be split across chunks.
   * @param {string} chunk
   * @returns {SSEEvent[]}
   */
  push(chunk) {
    const out = [];
    let text = chunk;
    if (this.pendingCR) {
      this.pendingCR = false;
      if (text.startsWith('\n')) text = text.slice(1);
    }
    this.buffer += text;
    let start = 0;
    for (let i = 0; i < this.buffer.length; i++) {
      const ch = this.buffer[i];
      if (ch !== '\n' && ch !== '\r') continue;
      const line = this.buffer.slice(start, i);
      if (ch === '\r') {
        if (i + 1 < this.buffer.length) {
          if (this.buffer[i + 1] === '\n') i++;
        } else {
          this.pendingCR = true;
        }
      }
      start = i + 1;
      this.line(line, out);
    }
    this.buffer = this.buffer.slice(start);
    return out;
  }

  line(line, out) {
    if (line === '') {
      if (this.data.length > 0) {
        out.push({ type: this.type || 'message', data: this.data.join('\n'), id: this.hasId ? this.id : null });
      }
      this.data = [];
      this.type = '';
      this.hasId = false;
      return;
    }
    if (line.startsWith(':')) return;
    const colon = line.indexOf(':');
    let field = line;
    let value = '';
    if (colon >= 0) {
      field = line.slice(0, colon);
      value = line.slice(colon + 1);
      if (value.startsWith(' ')) value = value.slice(1);
    }
    switch (field) {
      case 'data':
        this.data.push(value);
        break;
      case 'event':
        this.type = value;
        break;
      case 'id':
        if (!value.includes('\0')) {
          this.id = value;
          this.hasId = true;
        }
        break;
      default:
        // "retry" and unknown fields are ignored: reconnect timing is the client's own backoff.
        break;
    }
  }
}
