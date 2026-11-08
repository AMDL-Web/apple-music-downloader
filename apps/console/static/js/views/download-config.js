// New download: the server's download defaults (GET config) the Options card
// edits against. They load on mount and again after a reconnect (the shell's
// epoch) or on Retry.

import { useState, useEffect } from '../ui/preact.js';
import { getConfig } from '../api.js';

/** {cfg, setCfgRun}: cfg is {loading, data, error}; setCfgRun((n) => n + 1) reads the config again. */
export function useServerConfig(epoch) {
  const [cfg, setCfg] = useState({ loading: true, data: null, error: null });
  const [cfgRun, setCfgRun] = useState(0);
  useEffect(() => {
    let off = false;
    setCfg((c) => ({ ...c, loading: true }));
    getConfig().then(
      (data) => !off && setCfg({ loading: false, data, error: null }),
      (error) => !off && setCfg((c) => ({ loading: false, data: c.data, error })),
    );
    return () => {
      off = true;
    };
  }, [cfgRun, epoch]);
  return { cfg, setCfgRun };
}
