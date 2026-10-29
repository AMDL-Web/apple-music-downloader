// Saved values that changed in another tab or in the file: look at the
// server's values again when the tab comes back or reconnects, and notify
// quietly on refresh or show a choice under unsaved edits.

import { useState, useEffect, useRef, useCallback } from '../ui/preact.js';
import { getConfig } from '../api.js';
import { flattenConfig, diffConfig, labelList } from '../lib/settings-draft.js';

/** The notice for saved values that changed elsewhere while edits are pending. */
export function elsewhereText(keys, edits) {
  const changed = `The saved ${keys.length === 1 ? 'value' : 'values'} of ${labelList(keys, 4)} changed in another tab or in the file.`;
  const mine = edits === 1 ? 'Your unsaved change stays pending' : `Your ${edits} unsaved changes stay pending`;
  return `${changed} ${mine}; saving sends only ${edits === 1 ? 'it' : 'those'}, and if the same setting changed elsewhere, yours wins.`;
}

/**
 * Look at the server's values again when the tab comes back, and after a
 * reconnect (epoch). Nothing changed: nothing happens. Changed with no
 * local edits: refresh quietly and say so. Changed under local edits: a
 * notice, and the draft stays. Save still sends only the edited keys, so
 * the last write wins.
 */
export function useElsewhere({ cfg, dirty, saving, apply, applySeq, setLoadSeq, setLive, epoch }) {
  // Saved values that changed in another tab or in the file: a quiet note
  // after a silent refresh, or (under unsaved edits) a notice with a choice.
  const [updatedNote, setUpdatedNote] = useState('');
  const [elsewhere, setElsewhere] = useState(null);

  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;

  const cfgRef = useRef(cfg);
  cfgRef.current = cfg;
  const savingRef = useRef(false);
  savingRef.current = saving;
  const checking = useRef(false);
  const lastCheck = useRef(0);
  const acked = useRef('');
  const checkElsewhere = useCallback(
    async (force) => {
      const before = cfgRef.current;
      // A load that failed is tried again when the tab returns.
      if (!before.loaded) {
        if (before.error && !dirtyRef.current) setLoadSeq((n) => n + 1);
        return;
      }
      if (checking.current || savingRef.current) return;
      if (!force && Date.now() - lastCheck.current < 1500) return;
      checking.current = true;
      lastCheck.current = Date.now();
      const seq = applySeq.current;
      try {
        const res = await getConfig();
        if (seq !== applySeq.current) return;
        const next = flattenConfig(res && res.config);
        const changed = diffConfig(cfgRef.current.base, next);
        if (changed.length === 0) return;
        if (!dirtyRef.current) {
          apply(res);
          const note = `Updated from another tab or the file: ${labelList(changed, 4)} changed.`;
          setUpdatedNote(note);
          setLive(note);
        } else {
          const sig = JSON.stringify(changed.map((k) => [k, next[k]]));
          if (sig !== acked.current) {
            setElsewhere({ res, keys: changed, sig });
            setUpdatedNote('');
            setLive(`The saved values of ${labelList(changed, 4)} changed elsewhere. Your unsaved changes stay pending.`);
          }
        }
      } catch {
        // A failed look is not worth a message; the next focus tries again.
      } finally {
        checking.current = false;
      }
    },
    [apply, applySeq, setLive, setLoadSeq],
  );
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible') checkElsewhere(false);
    };
    const onFocus = () => checkElsewhere(false);
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onFocus);
    return () => {
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onFocus);
    };
  }, [checkElsewhere]);
  const seenEpoch = useRef(epoch);
  useEffect(() => {
    if (seenEpoch.current === epoch) return;
    seenEpoch.current = epoch;
    if (cfgRef.current.loaded) checkElsewhere(true);
    else if (!dirtyRef.current) setLoadSeq((n) => n + 1);
  }, [epoch, checkElsewhere, setLoadSeq]);
  // The notices are moot once nothing is edited: a save or discard shows the server's values.
  useEffect(() => {
    if (!dirty) setElsewhere(null);
  }, [dirty]);

  return { updatedNote, setUpdatedNote, elsewhere, setElsewhere, acked };
}
