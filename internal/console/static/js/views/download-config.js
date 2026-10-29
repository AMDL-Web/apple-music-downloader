// New download: the server's download defaults (GET config) and hooks list
// (GET hooks) the Options card edits against. Both load on mount and again
// after a reconnect (the shell's epoch); the config also on Retry.

import { useState, useEffect } from '../ui/preact.js';
import { getConfig, getHooks } from '../api.js';

/** {cfg, setCfgRun, hooks}: each {loading, data, error}; setCfgRun((n) => n + 1) reads the config again. */
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
  const [hooks, setHooks] = useState({ loading: true, data: null, error: null });
  useEffect(() => {
    let off = false;
    getHooks().then(
      (data) => !off && setHooks({ loading: false, data, error: null }),
      (error) => !off && setHooks({ loading: false, data: null, error }),
    );
    return () => {
      off = true;
    };
  }, [epoch]);
  return { cfg, setCfgRun, hooks };
}
