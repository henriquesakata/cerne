import { configure } from '@testing-library/react'

// findBy*/waitFor give up after 1 s by default. On a loaded machine — parallel
// agents, a Go suite alongside — a render can take longer, and the suite
// flaked: 2 of 10 loaded runs, each a findByText at ~1.1 s (F-0491 review).
// 3 s stays under vitest's 5 s test timeout and costs nothing on a pass.
configure({ asyncUtilTimeout: 3000 })
