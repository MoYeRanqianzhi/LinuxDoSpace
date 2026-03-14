// paymentTrackingStorageKey stores a short newest-first queue of recently
// created Linux Do Credit order numbers. The dedicated callback page uses this
// queue to recover the most likely order even when the gateway returns without
// explicit query parameters and another tab has already finished one order.
const paymentTrackingStorageKey = 'linuxdospace.payment.recent-order-nos';
const maxRememberedOrderCount = 10;

// rememberLatestPaymentOrder prepends one order number into the recent-order
// queue. Failures are ignored because the checkout still works without the
// convenience fallback.
export function rememberLatestPaymentOrder(outTradeNo: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    const normalizedOrderNo = outTradeNo.trim();
    if (!normalizedOrderNo) {
      return;
    }
    const items = readRememberedPaymentOrders().filter((item) => item !== normalizedOrderNo);
    items.unshift(normalizedOrderNo);
    window.localStorage.setItem(paymentTrackingStorageKey, JSON.stringify(items.slice(0, maxRememberedOrderCount)));
  } catch {
    // Ignore browser storage failures.
  }
}

// readRememberedPaymentOrders returns the current recent-order queue.
export function readRememberedPaymentOrders(): string[] {
  if (typeof window === 'undefined') {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(paymentTrackingStorageKey);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .map((item) => String(item).trim())
      .filter((item, index, items) => item !== '' && items.indexOf(item) === index)
      .slice(0, maxRememberedOrderCount);
  } catch {
    return [];
  }
}

// readRememberedPaymentOrder returns the newest remembered order number.
export function readRememberedPaymentOrder(): string {
  return readRememberedPaymentOrders()[0] ?? '';
}

// clearRememberedPaymentOrder removes one known order number from the recent
// queue. When no order number is provided, the whole queue is cleared.
export function clearRememberedPaymentOrder(outTradeNo?: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    if (!outTradeNo || outTradeNo.trim() === '') {
      window.localStorage.removeItem(paymentTrackingStorageKey);
      return;
    }

    const normalizedOrderNo = outTradeNo.trim();
    const nextItems = readRememberedPaymentOrders().filter((item) => item !== normalizedOrderNo);
    if (nextItems.length === 0) {
      window.localStorage.removeItem(paymentTrackingStorageKey);
      return;
    }
    window.localStorage.setItem(paymentTrackingStorageKey, JSON.stringify(nextItems));
  } catch {
    // Ignore browser storage failures.
  }
}
