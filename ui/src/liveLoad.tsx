import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { useT } from './i18n'
import { useLiveVersion } from './live'

// Shared screen loading (F-0300), extracted from Manage (F-0296..F-0299) so
// every screen fails the same way: content already on screen stays, with a
// notice and a retry; with no content yet, an error page with the retry.

export type LiveLoad<T> = {
  // The loaded value for the CURRENT deps, or null (not loaded yet / deps changed).
  data: T | null
  // The last applied failure for the current deps, or null.
  error: string | null
  // Fetch again; settles (never rejects) once the response is applied or dropped.
  reload: () => Promise<void>
  // reload() for the "try again" button: `retrying` locks it while it runs.
  retry: () => void
  retrying: boolean
}

type Key = readonly unknown[]

const sameKey = (a: Key, b: Key) => a.length === b.length && a.every((v, i) => Object.is(v, b[i]))

// useLiveLoad fetches on mount, whenever `deps` change and whenever the
// backend advances (live version). Responses may land out of order: one older
// than the last applied is dropped, data or error — not "only the latest
// issued wins", which would freeze the screen while the latest is in flight
// (F-0298). A value or failure belongs to the deps it was fetched for, so a
// subject change (e.g. another id) hides the previous subject at once.
// `live: false` opts out of the live re-fetch for content the backend advance
// does not change right away (the wiki, compiled afterwards).
export function useLiveLoad<T>(
  fetcher: () => Promise<T>,
  deps: Key = [],
  { live = true }: { live?: boolean } = {},
): LiveLoad<T> {
  const [loaded, setLoaded] = useState<{ key: Key; data: T } | null>(null)
  const [failed, setFailed] = useState<{ key: Key; message: string } | null>(null)
  const [retryingFor, setRetryingFor] = useState<Key | null>(null)
  const liveVersion = useLiveVersion()
  const version = live ? liveVersion : 0
  const issued = useRef(0)
  const applied = useRef(0)
  // The fetcher and subject of the latest render, read when a load is issued
  // and again when its response lands. A reload captured by an older render
  // (a save resolving after the user moved to another id) therefore reloads
  // the CURRENT subject, and a response for a subject no longer on screen is
  // dropped instead of evicting the one that is.
  const latest = useRef({ fetcher, deps })
  latest.current = { fetcher, deps }

  const reload = useCallback(() => {
    const seq = ++issued.current
    const { fetcher, deps: key } = latest.current
    const fresh = () => {
      if (!sameKey(key, latest.current.deps) || seq < applied.current) return false
      applied.current = seq
      return true
    }
    return fetcher()
      .then((data) => {
        if (!fresh()) return
        // A newer value supersedes an older failure, as it does older data.
        setFailed(null)
        setLoaded({ key, data })
      })
      .catch((e: unknown) => {
        if (fresh()) setFailed({ key, message: e instanceof Error ? e.message : String(e) })
      })
  }, [])

  const retry = useCallback(() => {
    const key = latest.current.deps
    setRetryingFor(key)
    void reload().finally(() => setRetryingFor((k) => (k === key ? null : k)))
  }, [reload])

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version, ...deps])

  return {
    data: loaded && sameKey(loaded.key, deps) ? loaded.data : null,
    error: failed && sameKey(failed.key, deps) ? failed.message : null,
    reload,
    retry,
    // The lock belongs to the subject that was retried, not to whatever is shown.
    retrying: retryingFor !== null && sameKey(retryingFor, deps),
  }
}

// LoadPending is the whole-screen state while there is no content: the
// failure with a retry, or "loading". `message` formats the screen's own
// load-error text. The failure is the same Notice the content gets (F-0302),
// so the page says as much to a screen reader, and keeps the focus the same way.
export function LoadPending<T>({
  load,
  message,
}: {
  load: LiveLoad<T>
  message: (error: string) => string
}) {
  const { t } = useT()
  if (load.error === null) return <p className="status">{t.common.loading}</p>
  return (
    <div className="status">
      <Notice message={message(load.error)} onRetry={load.retry} busy={load.retrying} />
    </div>
  )
}

// Notice sits over content that stays on screen after something failed: the
// message and a retry, locked while `busy`. Not tied to a load (F-0301) — a
// layout that could not be computed uses it too. The lock is aria-disabled,
// not disabled (F-0302): a disabled button leaves the focus order, and the
// keyboard user who just pressed it is thrown back to the top of the document.
// autoFocus hands the focus to the retry when the notice replaces the control
// the user just pressed (F-0316): that control unmounts, and the focus would
// otherwise fall to <body>.
export function Notice({
  message,
  onRetry,
  busy = false,
  autoFocus = false,
}: {
  message: string
  onRetry: () => void
  busy?: boolean
  autoFocus?: boolean
}) {
  const { t } = useT()
  // Two notices may be up at once, their retries named alike: each button is
  // described by its own message.
  const id = useId()
  // The notice goes away when the retry works. If its button had the focus
  // (kept through the attempt by aria-disabled), the focus would fall to the
  // document: hand it to the screen's <main> instead — the one around the
  // notice, or the one that just replaced the failure page. Read in a layout
  // cleanup, while the button is still in the document; focused after the
  // commit, when the new <main> exists — and only if the focus did fall:
  // content that focused something of its own keeps it. The <main> is
  // focusable for this hand-off alone, not left behind as a click target.
  const retry = useRef<HTMLButtonElement>(null)
  useLayoutEffect(() => {
    const button = retry.current
    return () => {
      if (button === null || document.activeElement !== button) return
      const around = button.closest('main')
      queueMicrotask(() => {
        const now = document.activeElement
        if (now !== null && now !== document.body) return
        const main = around?.isConnected ? around : document.querySelector('main')
        if (main === null) return
        main.tabIndex = -1
        main.addEventListener('blur', () => main.removeAttribute('tabindex'), { once: true })
        main.focus({ preventScroll: true })
      })
    }
  }, [])
  return (
    <div className="load-notice" role="status">
      <p id={id} className="form-error">
        {message}
      </p>
      <button
        ref={retry}
        type="button"
        autoFocus={autoFocus}
        aria-describedby={id}
        aria-disabled={busy || undefined}
        onClick={busy ? undefined : onRetry}
      >
        {/* The label changes inside the status region, so the wait is said,
            not only dimmed — and so is its end, when the label comes back
            over a failure that reads exactly like the previous one. */}
        {busy ? t.common.retrying : t.common.retry}
      </button>
    </div>
  )
}

// LoadNotice is the Notice of a failed refresh. Renders nothing while there
// is no failure.
export function LoadNotice<T>({ load, message }: { load: LiveLoad<T>; message?: string }) {
  const { t } = useT()
  if (load.error === null) return null
  return (
    <Notice
      message={message ?? t.common.reloadError(load.error)}
      onRetry={load.retry}
      busy={load.retrying}
    />
  )
}
