import { useEffect, useState, useRef } from 'react'
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
  const [retrying, setRetrying] = useState(false)
  const liveVersion = useLiveVersion()
  const version = live ? liveVersion : 0
  const issued = useRef(0)
  const applied = useRef(0)

  const reload = () => {
    const seq = ++issued.current
    const key = deps
    const fresh = () => {
      if (seq < applied.current) return false
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
  }

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version, ...deps])

  return {
    data: loaded && sameKey(loaded.key, deps) ? loaded.data : null,
    error: failed && sameKey(failed.key, deps) ? failed.message : null,
    reload,
    retry: () => {
      setRetrying(true)
      void reload().finally(() => setRetrying(false))
    },
    retrying,
  }
}

function Retry<T>({ load }: { load: LiveLoad<T> }) {
  const { t } = useT()
  return (
    <button type="button" disabled={load.retrying} onClick={load.retry}>
      {t.common.retry}
    </button>
  )
}

// LoadPending is the whole-screen state while there is no content: the
// failure with a retry, or "loading". `message` formats the screen's own
// load-error text.
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
    <p className="status error">
      {message(load.error)} <Retry load={load} />
    </p>
  )
}

// LoadNotice sits over content that stays on screen after a failed refresh.
// Renders nothing while there is no failure.
export function LoadNotice<T>({ load, message }: { load: LiveLoad<T>; message?: string }) {
  const { t } = useT()
  if (load.error === null) return null
  return (
    <div className="load-notice" role="status">
      <p className="form-error">{message ?? t.common.reloadError(load.error)}</p>
      <Retry load={load} />
    </div>
  )
}
