/** debounce delays fn until calls stop for ms. */
export function debounce<A extends unknown[]>(fn: (...a: A) => void, ms = 500) {
  let t = 0
  return (...a: A) => {
    clearTimeout(t)
    t = window.setTimeout(() => fn(...a), ms)
  }
}
