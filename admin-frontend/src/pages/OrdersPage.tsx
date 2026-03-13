import { useEffect, useMemo, useState } from 'react';
import { CreditCard, ExternalLink, LoaderCircle, RefreshCw, Search } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { APIError, getAdminPaymentOrder, listAdminPaymentOrders } from '../lib/api';
import { GlassCard } from '../components/GlassCard';
import type { AdminPaymentOrder } from '../types/admin';

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

function readableStatus(order: AdminPaymentOrder): { label: string; className: string } {
  if (order.status === 'paid' && order.applied_at) {
    return { label: '已支付并发放', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' };
  }
  switch (order.status) {
    case 'paid':
      return { label: '已支付，待发放', className: 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300' };
    case 'pending':
      return { label: '待支付', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' };
    case 'failed':
      return { label: '创建失败', className: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300' };
    case 'refunded':
      return { label: '已退款', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
    default:
      return { label: '已创建', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
}

function shouldRefresh(order: AdminPaymentOrder): boolean {
  return order.status === 'created' || order.status === 'pending' || (order.status === 'paid' && !order.applied_at);
}

interface OrdersPageProps {
  csrfToken: string;
}

// OrdersPage renders the administrator-facing Linux Do Credit order list so
// operators can inspect current payment flow health and refresh one order.
export function OrdersPage({ csrfToken: _csrfToken }: OrdersPageProps) {
  const [orders, setOrders] = useState<AdminPaymentOrder[]>([]);
  const [keyword, setKeyword] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refreshingOrderNo, setRefreshingOrderNo] = useState('');

  const filteredOrders = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase();
    if (!normalizedKeyword) {
      return orders;
    }
    return orders.filter((order) =>
      [
        order.username,
        order.display_name,
        order.product_name,
        order.out_trade_no,
        order.provider_trade_no,
        order.status,
      ].some((field) => field.toLowerCase().includes(normalizedKeyword)),
    );
  }, [keyword, orders]);

  useEffect(() => {
    void loadOrders();
  }, []);

  async function loadOrders(): Promise<void> {
    try {
      setLoading(true);
      const items = await listAdminPaymentOrders();
      setOrders(items);
      setError('');
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载订单列表失败。');
    } finally {
      setLoading(false);
    }
  }

  async function refreshOrder(outTradeNo: string): Promise<void> {
    try {
      setRefreshingOrderNo(outTradeNo);
      const item = await getAdminPaymentOrder(outTradeNo);
      setOrders((current) =>
        current
          .map((order) => (order.out_trade_no === item.out_trade_no ? item : order))
          .sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime()),
      );
      setError('');
    } catch (refreshError) {
      setError(refreshError instanceof APIError ? refreshError.message : '刷新订单状态失败。');
    } finally {
      setRefreshingOrderNo('');
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-emerald-100 p-3 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
            <CreditCard size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">订单管理</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">查看所有 Linux Do Credit 订单，并在需要时刷新单个订单的上游状态。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-96">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索用户名、订单号、商品或状态"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-emerald-400 focus:ring-2 focus:ring-emerald-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      {error ? (
        <div className="mb-5 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
          {error}
        </div>
      ) : null}

      <GlassCard className="overflow-hidden p-0">
        <div className="custom-scrollbar overflow-x-auto">
          <table className="min-w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-white/5">
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">下单用户</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">商品</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">金额 / 数量</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">状态</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">时间</th>
                <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                    正在加载订单列表...
                  </td>
                </tr>
              ) : null}

              {!loading ? (
                <AnimatePresence>
                  {filteredOrders.map((order) => {
                    const status = readableStatus(order);
                    const refreshing = refreshingOrderNo === order.out_trade_no;

                    return (
                      <motion.tr
                        key={order.out_trade_no}
                        layout
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, x: -30 }}
                        className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5"
                      >
                        <td className="px-5 py-4 align-top">
                          <div className="font-semibold text-slate-900 dark:text-white">{order.username}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{order.display_name || '无昵称'}</div>
                          <div className="mt-1 font-mono text-[11px] text-slate-400">{order.out_trade_no}</div>
                        </td>
                        <td className="px-5 py-4 align-top">
                          <div className="font-semibold text-slate-900 dark:text-white">{order.product_name}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{order.title}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                            {order.provider_trade_no ? `上游单号：${order.provider_trade_no}` : '尚未回填上游单号'}
                          </div>
                        </td>
                        <td className="px-5 py-4 align-top text-slate-700 dark:text-slate-200">
                          <div>{formatLDC(order.total_price_cents)} LDC</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                            {order.units} 份，共发放 {order.granted_total.toLocaleString('zh-CN')} {order.grant_unit}
                          </div>
                        </td>
                        <td className="px-5 py-4 align-top">
                          <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${status.className}`}>{status.label}</span>
                        </td>
                        <td className="px-5 py-4 align-top text-slate-600 dark:text-slate-300">
                          <div>创建：{formatDate(order.created_at)}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">支付：{formatDate(order.paid_at)}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">发放：{formatDate(order.applied_at)}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">检查：{formatDate(order.last_checked_at)}</div>
                        </td>
                        <td className="px-5 py-4 align-top">
                          <div className="flex justify-end gap-2">
                            {order.payment_url ? (
                              <button
                                type="button"
                                onClick={() => window.open(order.payment_url, '_blank', 'noopener,noreferrer')}
                                className="rounded-xl p-2 text-emerald-500 transition hover:bg-emerald-100 dark:hover:bg-emerald-900/25"
                                aria-label={`打开订单 ${order.out_trade_no} 的支付页`}
                              >
                                <ExternalLink size={16} />
                              </button>
                            ) : null}
                            {shouldRefresh(order) ? (
                              <button
                                type="button"
                                onClick={() => void refreshOrder(order.out_trade_no)}
                                disabled={refreshing}
                                className="inline-flex items-center gap-2 rounded-xl bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-700 transition hover:bg-slate-200 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
                              >
                                {refreshing ? <LoaderCircle size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                                {refreshing ? '刷新中...' : '刷新'}
                              </button>
                            ) : null}
                          </div>
                        </td>
                      </motion.tr>
                    );
                  })}
                </AnimatePresence>
              ) : null}

              {!loading && filteredOrders.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                    当前没有符合条件的订单记录。
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </GlassCard>
    </div>
  );
}
