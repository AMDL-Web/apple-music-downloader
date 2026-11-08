// The one import site of the vendored Preact+htm bundle. The bundle has no
// Fragment export: return arrays (with keys) where a component has several
// roots.
export {
  html,
  render,
  h,
  Component,
  createContext,
  useState,
  useReducer,
  useEffect,
  useLayoutEffect,
  useRef,
  useMemo,
  useCallback,
  useContext,
} from '../../vendor/htm-preact-standalone-3.1.1.mjs';
