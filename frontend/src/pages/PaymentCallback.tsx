import { useEffect, useRef, useState } from 'react';
import { motion } from 'motion/react';
import { ArrowRight, CheckCircle2, Clock3, CreditCard, LoaderCircle, RefreshCw, ShieldAlert, XCircle } from 'lucide-react';
import { GlassCard } from '../components/GlassCard';
import { APIError, listMyPaymentOrders, refreshMyPaymentOrder } from '../lib/api';
import { clearRememberedPaymentOrder } from '../lib/payment-tracking';
import type { PaymentOrder, User } from '../types/api';

interface PaymentCallbackProps {
  authenticated: boolean;
  sessionLoading: boolean;
  user?: User;
  csrfToken?: string;
  onLogin: () => void;
  onOpenPermissions: () => void;
}

type CallbackTone = 'info' | 'success' | 'error';

interface CallbackNotice {
  tone: CallbackTone;
  title: string;
  message: string;
}

const maxRefreshAttempts = 20;
const refreshIntervalMilliseconds = 3000;

// PaymentCallback is the dedicated browser return route for Linux Do Credit.
// It recovers the expected order number, refreshes the order explicitly, and
// then shows a focused payment-state notification instead of dropping users
// back into the crowded permissions screen immediately.
export function PaymentCallback({
  authenticated,
  sessionLoading,
  user,
  csrfToken,
  onLogin,
  onOpenPermissions,
}: PaymentCallbackProps) {
  const [order, setOrder] = useState<PaymentOrder | null>(null);
  const [resolvedOrderNo, setResolvedOrderNo] = useState('');
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState<CallbackNotice>({
    tone: 'info',
    title: '正在核对支付结果',
    message: '请稍候，系统正在检查订单状态并等待后端确认权益发放。',
  });
  const [refreshAttempt, setRefreshAttempt] = useState(0);
  const refreshTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (sessionLoading) {
      return;
    }
    if (!authenticated || !csrfToken) {
      setLoading(false);
      setNotice({
        tone: 'info',
        title: '需要登录后继续核对',
        message: '支付页面已经返回，但订单核对需要当前浏览器仍然保持登录状态。重新登录后会继续回到这个回调页。',
      });
      return;
    }

    void resolveAndRefreshOrder();
  }, [authenticated, csrfToken, sessionLoading]);

  async function resolveAndRefreshOrder(): Promise<void> {
    try {
      setLoading(true);
      const candidateOrderNo = await resolveCandidateOrderNo();
      if (!candidateOrderNo) {
        setResolvedOrderNo('');
        setNotice({
          tone: 'error',
          title: '没有找到可核对的订单',
          message: '支付页面已经返回，但当前浏览器里没有找到对应订单号。你可以回到权限页查看全部订单后手动刷新。',
        });
        return;
      }

      setResolvedOrderNo(candidateOrderNo);
      await refreshOrder(candidateOrderNo, 0);
    } finally {
      setLoading(false);
    }
  }

  async function resolveCandidateOrderNo(): Promise<string> {
    const orders = await listMyPaymentOrders();
    const matchedOrder = findBestMatchingOrder(orders);
    return matchedOrder?.out_trade_no ?? '';
  }

  async function refreshOrder(outTradeNo: string, nextAttempt: number): Promise<void> {
    if (!csrfToken) {
      return;
    }

    try {
      const refreshedOrder = await refreshMyPaymentOrder(outTradeNo, csrfToken);
      setOrder(refreshedOrder);
      setRefreshAttempt(nextAttempt);
      setNotice(buildCallbackNotice(refreshedOrder, nextAttempt));

      if (isTerminalPaymentState(refreshedOrder)) {
        clearRememberedPaymentOrder(refreshedOrder.out_trade_no);
        return;
      }

      if (nextAttempt + 1 < maxRefreshAttempts) {
        refreshTimerRef.current = window.setTimeout(() => {
          void refreshOrder(outTradeNo, nextAttempt + 1);
        }, refreshIntervalMilliseconds);
        return;
      }

      setNotice({
        tone: 'info',
        title: '支付状态仍在同步中',
        message: `订单 ${refreshedOrder.out_trade_no} 还没有进入最终状态。异步通知可能仍在路上，你可以稍后手动再检查一次，或回到权限页查看全部订单。`,
      });
    } catch (error) {
      const fallbackOrder = await tryLoadFallbackOrder();
      if (fallbackOrder) {
        setOrder(fallbackOrder);
        setResolvedOrderNo(fallbackOrder.out_trade_no);
        setNotice(buildCallbackNotice(fallbackOrder, nextAttempt));
        if (isTerminalPaymentState(fallbackOrder)) {
          clearRememberedPaymentOrder(fallbackOrder.out_trade_no);
          return;
        }
        if (nextAttempt + 1 < maxRefreshAttempts) {
          refreshTimerRef.current = window.setTimeout(() => {
            void refreshOrder(fallbackOrder.out_trade_no, nextAttempt + 1);
          }, refreshIntervalMilliseconds);
          return;
        }
      }
      setNotice({
        tone: 'error',
        title: '订单核对失败',
        message: readableErrorMessage(error, '回调页暂时无法确认订单状态。你可以稍后回到权限页，在“查看全部订单”中手动刷新。'),
      });
    }
  }

  async function tryLoadFallbackOrder(): Promise<PaymentOrder | null> {
    try {
      const orders = await listMyPaymentOrders();
      const matchedOrder = findBestMatchingOrder(orders, resolvedOrderNo);
      return matchedOrder ?? null;
    } catch {
      return null;
    }
  }

  return (
    <div className="mx-auto max-w-4xl px-6 pb-24 pt-32">
      <motion.div initial={{ y: 20, opacity: 0 }} animate={{ y: 0, opacity: 1 }}>
        <GlassCard className="overflow-hidden">
          <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <div className="inline-flex items-center gap-2 rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                <CreditCard size={14} />
                Linux Do Credit 支付回调
              </div>
              <h1 className="mt-4 text-3xl font-extrabold text-gray-900 dark:text-white md:text-4xl">正在确认你的支付结果</h1>
              <p className="mt-3 max-w-2xl text-sm leading-7 text-gray-600 dark:text-gray-300">
                这是专门的支付回调页。当前支付平台不会回传订单参数，所以页面会直接以你账号下的最新订单为准，主动向后端刷新状态，并等待异步通知与权益发放完成。
              </p>
            </div>
            <div className="rounded-[1.5rem] border border-white/20 bg-white/45 px-5 py-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
              <div>当前账号：{authenticated ? user?.username ?? '已登录' : '未登录'}</div>
              <div>当前策略：以最新订单为准</div>
              <div>回调路由：/payments/callback</div>
            </div>
          </div>

          <div className="mt-8 grid gap-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(280px,0.85fr)]">
            <div className="rounded-[1.75rem] border border-white/20 bg-white/35 p-6 dark:border-white/10 dark:bg-black/20">
              <div className="flex items-center gap-3">
                <div className={`rounded-2xl p-3 ${notice.tone === 'success' ? 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300' : notice.tone === 'error' ? 'bg-red-100 text-red-600 dark:bg-red-900/30 dark:text-red-300' : 'bg-sky-100 text-sky-600 dark:bg-sky-900/30 dark:text-sky-300'}`}>
                  {notice.tone === 'success' ? <CheckCircle2 size={22} /> : notice.tone === 'error' ? <XCircle size={22} /> : loading ? <LoaderCircle size={22} className="animate-spin" /> : <Clock3 size={22} />}
                </div>
                <div>
                  <div className="text-lg font-bold text-gray-900 dark:text-white">{notice.title}</div>
                  <div className="mt-1 text-sm text-gray-600 dark:text-gray-300">{notice.message}</div>
                </div>
              </div>

              <div className="mt-6 grid gap-3 md:grid-cols-2">
                <InfoStat title="实际核对订单号" value={resolvedOrderNo || '尚未确定'} mono />
                <InfoStat title="刷新轮次" value={`${refreshAttempt + 1} / ${maxRefreshAttempts}`} />
                <InfoStat title="状态来源" value="账号下最新订单" />
                <InfoStat title="订单状态" value={order ? readablePaymentStatus(order) : '尚未取得'} />
              </div>

              {!authenticated && !sessionLoading ? (
                <div className="mt-6">
                  <button
                    type="button"
                    onClick={onLogin}
                    className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-5 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700"
                  >
                    <ArrowRight size={16} />
                    重新登录并继续核对
                  </button>
                </div>
              ) : null}
            </div>

            <div className="rounded-[1.75rem] border border-white/20 bg-white/35 p-6 dark:border-white/10 dark:bg-black/20">
              <div className="text-sm font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">订单摘要</div>
              {order ? (
                <div className="mt-4 space-y-3 text-sm leading-7 text-gray-700 dark:text-gray-200">
                  <div>项目：<span className="font-semibold text-gray-900 dark:text-white">{order.product_name}</span></div>
                  <div>订单号：<span className="font-mono text-xs">{order.out_trade_no}</span></div>
                  <div>金额：<span className="font-semibold">{formatLDC(order.total_price_cents)} LDC</span></div>
                  <div>创建时间：{formatDate(order.created_at)}</div>
                  <div>支付时间：{formatDate(order.paid_at)}</div>
                  <div>发放时间：{formatDate(order.applied_at)}</div>
                </div>
              ) : (
                <div className="mt-4 text-sm leading-7 text-gray-600 dark:text-gray-300">
                  当前还没有拿到订单详情。系统会直接从你账号下的最新订单开始核对。
                </div>
              )}

              <div className="mt-6 flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={onOpenPermissions}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-5 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700"
                >
                  <ArrowRight size={16} />
                  返回权限页
                </button>
                {resolvedOrderNo && authenticated && csrfToken ? (
                  <button
                    type="button"
                    onClick={() => void refreshOrder(resolvedOrderNo, refreshAttempt)}
                    className="inline-flex items-center gap-2 rounded-2xl border border-white/20 bg-white/70 px-5 py-3 text-sm font-semibold text-gray-900 transition hover:bg-white dark:border-white/10 dark:bg-black/35 dark:text-white dark:hover:bg-black/50"
                  >
                    <RefreshCw size={16} />
                    立即再检查一次
                  </button>
                ) : null}
              </div>
            </div>
          </div>
        </GlassCard>
      </motion.div>
    </div>
  );
}

function buildCallbackNotice(order: PaymentOrder, attempt: number): CallbackNotice {
  if (order.status === 'paid' && order.applied_at) {
    return {
      tone: 'success',
      title: '支付成功，权益已经到账',
      message: `订单 ${order.out_trade_no} 已确认支付，对应权益已经发放。你现在可以返回权限页继续操作。`,
    };
  }

  if (order.status === 'paid') {
    return {
      tone: 'info',
      title: '支付已确认，正在等待权益发放',
      message: `订单 ${order.out_trade_no} 已经支付成功，但后端还在完成最后的权益落库。当前是第 ${attempt + 1} 次检查。`,
    };
  }

  if (order.status === 'failed') {
    return {
      tone: 'error',
      title: '订单创建或支付失败',
      message: `订单 ${order.out_trade_no} 当前已被标记为失败。你可以回到权限页重新创建订单，或稍后查看全部订单。`,
    };
  }

  if (order.status === 'refunded') {
    return {
      tone: 'error',
      title: '订单已退款',
      message: `订单 ${order.out_trade_no} 当前状态为已退款。`,
    };
  }

  return {
    tone: 'info',
    title: '支付页面已返回，等待后端同步',
    message: `最新订单 ${order.out_trade_no} 当前仍然是 ${readablePaymentStatus(order)}。如果你已经完成支付，通常只需要再等待几秒。当前是第 ${attempt + 1} 次检查。`,
  };
}

function isTerminalPaymentState(order: PaymentOrder): boolean {
  return (order.status === 'paid' && Boolean(order.applied_at)) || order.status === 'failed' || order.status === 'refunded';
}

function readablePaymentStatus(order: PaymentOrder): string {
  if (order.status === 'paid' && order.applied_at) {
    return '已支付并发放';
  }
  switch (order.status) {
    case 'paid':
      return '已支付，待发放';
    case 'pending':
      return '待支付';
    case 'failed':
      return '失败';
    case 'refunded':
      return '已退款';
    default:
      return '已创建';
  }
}

function InfoStat({ title, value, mono = false }: { title: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/45 p-4 dark:border-white/10 dark:bg-black/25">
      <div className="text-xs font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className={`mt-2 font-semibold text-gray-900 dark:text-white ${mono ? 'font-mono text-sm break-all' : 'text-base'}`}>{value}</div>
    </div>
  );
}

function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}

function formatDate(value?: string): string {
  if (!value) {
    return '暂无';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function formatLDC(valueInCents: number): string {
  return (valueInCents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: valueInCents % 100 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  });
}

function findBestMatchingOrder(orders: PaymentOrder[], preferredOutTradeNo = ''): PaymentOrder | null {
  const normalizedPreferredOutTradeNo = preferredOutTradeNo.trim();
  if (normalizedPreferredOutTradeNo) {
    const exactMatch = orders.find((item) => item.out_trade_no === normalizedPreferredOutTradeNo);
    if (exactMatch) {
      return exactMatch;
    }
  }

  return orders[0] ?? null;
}
