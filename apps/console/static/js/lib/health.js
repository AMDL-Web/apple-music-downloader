// One readable line for an upstream failure (the session service, the
// database): the Settings heads and the New download notes show it, with
// the full text as the title. Pure.

/**
 * One readable line for an upstream failure. Dial errors arrive as nested
 * Go error chains that bury the cause; the full text stays available as the
 * view's title.
 */
export function summarizeError(message) {
  if (!message) return undefined;
  const text = String(message);
  const addr = /dial tcp ([^\s:]+:\d+)/.exec(text) || /invalid target address ([^\s,]+?)[,:]?(?:\s|$)/.exec(text);
  const at = addr ? ` (${addr[1]})` : '';
  if (/connection refused/i.test(text)) return `Connection refused${at}`;
  if (/no such host/i.test(text)) return `Host not found${at}`;
  if (/deadline exceeded|timed? ?out/i.test(text)) return `Timed out${at}`;
  if (/invalid target address/i.test(text)) return `Invalid address${at}`;
  const desc = text.replace(/^rpc error: code = \w+ desc = /, '');
  return desc.length > 80 ? `${desc.slice(0, 79)}…` : desc;
}
