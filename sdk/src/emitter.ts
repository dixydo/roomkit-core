type Listener<T> = (data: T) => void

/**
 * Tiny strongly-typed event emitter. Returns an unsubscribe function from
 * `on()`. Errors thrown by listeners are logged and swallowed so one
 * misbehaving handler doesn't break siblings.
 */
export class Emitter<TEvents extends Record<string, any>> {
  private listeners = new Map<keyof TEvents, Set<Listener<any>>>()

  on<K extends keyof TEvents>(event: K, handler: Listener<TEvents[K]>): () => void {
    let set = this.listeners.get(event)
    if (!set) {
      set = new Set()
      this.listeners.set(event, set)
    }
    set.add(handler)
    return () => { set!.delete(handler) }
  }

  off<K extends keyof TEvents>(event: K, handler: Listener<TEvents[K]>): void {
    this.listeners.get(event)?.delete(handler)
  }

  protected emit<K extends keyof TEvents>(event: K, data: TEvents[K]): void {
    const set = this.listeners.get(event)
    if (!set) return
    set.forEach((h) => {
      try { h(data) } catch (e) { console.error('[roomkit] event handler error', event, e) }
    })
  }
}
