// paymentTrackingStorageKey stores the latest locally created Linux Do Credit
// order number so the dedicated callback page can recover the correct order
// even when the gateway returns without explicit query parameters.
const paymentTrackingStorageKey = 'linuxdospace.payment.last-order-no';

// rememberLatestPaymentOrder stores the newest order number in browser-local
// storage. Failures are ignored because the checkout still works without the
// convenience fallback.
export function rememberLatestPaymentOrder(outTradeNo: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    window.localStorage.setItem(paymentTrackingStorageKey, outTradeNo.trim());
  } catch {
    // Ignore browser storage failures.
  }
}

// readRememberedPaymentOrder returns the last locally stored order number.
export function readRememberedPaymentOrder(): string {
  if (typeof window === 'undefined') {
    return '';
  }
  try {
    return window.localStorage.getItem(paymentTrackingStorageKey)?.trim() ?? '';
  } catch {
    return '';
  }
}

// clearRememberedPaymentOrder removes the stored fallback once the callback
// page confirms that the payment has reached a terminal state.
export function clearRememberedPaymentOrder(): void {
  if (typeof window === 'undefined') {
    return;
  }
  try {
    window.localStorage.removeItem(paymentTrackingStorageKey);
  } catch {
    // Ignore browser storage failures.
  }
}
