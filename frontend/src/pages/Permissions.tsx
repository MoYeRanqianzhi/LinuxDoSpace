import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { ArrowLeft, ArrowRight, CheckCircle2, Clock3, CreditCard, ExternalLink, Key, List, LoaderCircle, Send, ShieldAlert, ShieldPlus, Ticket, XCircle } from 'lucide-react';
import { POWBenefitSection } from '../components/POWBenefitSection';
import { GlassCard } from '../components/GlassCard';
import { GlassSelect, type GlassSelectOption } from '../components/GlassSelect';
import { APIError, createMyPaymentOrder, listMyPaymentOrders, listMyPermissions, listPublicPaymentProducts, refreshMyPaymentOrder } from '../lib/api';
import { clearRememberedPaymentOrder, rememberLatestPaymentOrder } from '../lib/payment-tracking';
import type { PaymentOrder, PaymentProduct, User, UserPermission } from '../types/api';

interface PermissionsProps {
  authenticated: boolean;
  sessionLoading: boolean;
  user?: User;
  csrfToken?: string;
  onLogin: () => void;
  onOpenEmails: () => void;
}

type ViewMode = 'main' | 'records';
type EntryStage = 'live' | 'planned';

interface CatalogItem {
  key: string;
  displayName: string;
  selectLabel: string;
  typeLabel: string;
  stage: EntryStage;
  description: string;
  hint: string;
  target: (user?: User) => string;
}

interface OverviewRow {
  item: CatalogItem;
  target: string;
  permission: UserPermission | null;
}

type NoticeTone = 'error' | 'success' | 'info';

interface PaymentNotice {
  tone: NoticeTone;
  message: string;
}

const emailCatchAllPermissionKey = 'email_catch_all';

// builtinCatalog 恢复原权限页的多入口结构，同时明确哪些入口已经真实接入。
const builtinCatalog: CatalogItem[] = [
  {
    key: emailCatchAllPermissionKey,
    displayName: '*@<用户名>.linuxdo.space',
    selectLabel: '二级域名邮箱泛解析',
    typeLabel: '邮箱泛解析',
    stage: 'live',
    description: '为与你用户名同名的默认二级域名开启整段邮件命名空间的泛解析转发。',
    hint: '登录后可查看真实权限状态，并前往邮箱页面提交申请或管理转发目标。',
    target: (user) => `*@${normalizeIdentity(user?.username ?? 'username')}.linuxdo.space`,
  },
  {
    key: 'single_allocation',
    displayName: '特定二级域名分发',
    selectLabel: '某个特定二级域名',
    typeLabel: '特定二级域名',
    stage: 'planned',
    description: '用于申请某个额外的特定二级域名，例如独立 API、展示页或服务入口。',
    hint: '该入口仍处于规划阶段，目前只恢复原页面结构，不会伪造提交成功。',
    target: () => 'api.linuxdo.space',
  },
  {
    key: 'quota_boost',
    displayName: '追加注册额度',
    selectLabel: '某个域名的任意 X 次注册',
    typeLabel: '追加额度',
    stage: 'planned',
    description: '用于申请更多可分配次数，适合需要管理多个子域名的用户。',
    hint: '该入口暂未开放，页面保留是为了恢复之前的设计而不是开放假功能。',
    target: () => '例如：额外 5 次注册额度',
  },
  {
    key: 'wildcard_subdomain',
    displayName: '二级域名及其全部子域名',
    selectLabel: '某个二级域名及其全部子域名（泛解析）',
    typeLabel: '泛解析',
    stage: 'planned',
    description: '用于申请某个二级域名下的整段命名空间，例如 `*.dev.linuxdo.space`。',
    hint: '该权限位目前只保留 UI 设计与展示位置，后续再接入审核与发放流程。',
    target: () => '*.dev.linuxdo.space',
  },
];

export function Permissions({ authenticated, sessionLoading, user, csrfToken, onLogin, onOpenEmails }: PermissionsProps) {
  const [viewMode, setViewMode] = useState<ViewMode>('main');
  const [permissions, setPermissions] = useState<UserPermission[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [paymentProducts, setPaymentProducts] = useState<PaymentProduct[]>([]);
  const [paymentProductsLoading, setPaymentProductsLoading] = useState(false);
  const [paymentProductsError, setPaymentProductsError] = useState('');
  const [paymentOrders, setPaymentOrders] = useState<PaymentOrder[]>([]);
  const [paymentOrdersLoading, setPaymentOrdersLoading] = useState(false);
  const [paymentOrdersError, setPaymentOrdersError] = useState('');
  const [paymentUnits, setPaymentUnits] = useState<Record<string, number>>({});
  const [creatingProductKey, setCreatingProductKey] = useState('');
  const [paymentNotice, setPaymentNotice] = useState<PaymentNotice | null>(null);
  const [pollingOrderNo, setPollingOrderNo] = useState('');
  const [selectedKey, setSelectedKey] = useState(emailCatchAllPermissionKey);
  const [redeemCode, setRedeemCode] = useState('');
  const [plannedTarget, setPlannedTarget] = useState(builtinCatalog[1].target());
  const [plannedReason, setPlannedReason] = useState('');
  const loadRequestTokenRef = useRef(0);
  const paymentProductsRequestTokenRef = useRef(0);
  const paymentOrdersRequestTokenRef = useRef(0);

  const permissionMap = useMemo(() => new Map(permissions.map((permission) => [permission.key, permission])), [permissions]);
  const catalog = useMemo(() => {
    const known = new Set(builtinCatalog.map((item) => item.key));
    const extras = permissions
      .filter((permission) => !known.has(permission.key))
      .map<CatalogItem>((permission) => ({
        key: permission.key,
        displayName: permission.display_name,
        selectLabel: permission.display_name,
        typeLabel: '系统权限',
        stage: 'live',
        description: permission.description || '后端返回的额外权限入口。',
        hint: '该权限由后端真实返回，页面会按当前状态直接展示。',
        target: () => permission.target || '由系统返回',
      }));
    return [...builtinCatalog, ...extras];
  }, [permissions]);
  const selectedItem = useMemo(() => catalog.find((item) => item.key === selectedKey) ?? catalog[0], [catalog, selectedKey]);
  const selectedPermission = useMemo(() => (selectedItem ? permissionMap.get(selectedItem.key) ?? null : null), [permissionMap, selectedItem]);
  const selectedTarget = selectedPermission?.target || selectedItem?.target(user) || '';
  const selectedStatus = selectedItem ? describeEntryStatus(selectedItem, selectedPermission) : describePermissionStatus('not_requested');
  const stageBadge = selectedItem ? describeStage(selectedItem.stage) : describeStage('planned');

  const options = useMemo<GlassSelectOption[]>(() => catalog.map((item) => ({ value: item.key, label: item.selectLabel })), [catalog]);
  const rows = useMemo<OverviewRow[]>(
    () => catalog.map((item) => ({ item, target: permissionMap.get(item.key)?.target || item.target(user), permission: permissionMap.get(item.key) ?? null })),
    [catalog, permissionMap, user],
  );
  const applicationRows = useMemo(
    () => rows.filter((row) => row.permission?.application).map((row) => ({ row, application: row.permission?.application! })),
    [rows],
  );

  useEffect(() => {
    if (!selectedItem || selectedItem.stage !== 'planned') return;
    setPlannedTarget(selectedItem.target(user));
    setPlannedReason('');
  }, [selectedItem, user]);

  useEffect(() => {
    void loadPaymentProducts();
  }, []);

  useEffect(() => {
    if (!authenticated) {
      loadRequestTokenRef.current += 1;
      paymentOrdersRequestTokenRef.current += 1;
      setPermissions([]);
      setLoading(false);
      setError('');
      setPaymentOrders([]);
      setPaymentOrdersLoading(false);
      setPaymentOrdersError('');
      setCreatingProductKey('');
      setPollingOrderNo('');
      setPaymentNotice(null);
      return;
    }
    void loadPermissions();
    void loadPaymentOrders();
  }, [authenticated]);

  useEffect(() => {
    if (!pollingOrderNo || !authenticated) return;

    const timer = window.setTimeout(() => {
      void refreshOnePaymentOrder(pollingOrderNo, true);
    }, 3000);
    return () => window.clearTimeout(timer);
  }, [authenticated, paymentOrders, pollingOrderNo]);

  useEffect(() => {
    setPaymentUnits((current) => {
      const next = { ...current };
      for (const item of paymentProducts) {
        if (!Number.isFinite(next[item.key]) || next[item.key] < 1) {
          next[item.key] = 1;
        }
      }
      return next;
    });
  }, [paymentProducts]);

  async function loadPermissions(): Promise<void> {
    const requestToken = ++loadRequestTokenRef.current;
    try {
      setLoading(true);
      const items = await listMyPermissions();
      if (requestToken !== loadRequestTokenRef.current) return;
      setPermissions(items);
      setError('');
    } catch (loadError) {
      if (requestToken !== loadRequestTokenRef.current) return;
      setPermissions([]);
      setError(readableErrorMessage(loadError, '无法加载权限列表。'));
    } finally {
      if (requestToken === loadRequestTokenRef.current) setLoading(false);
    }
  }

  async function loadPaymentProducts(): Promise<void> {
    const requestToken = ++paymentProductsRequestTokenRef.current;
    try {
      setPaymentProductsLoading(true);
      const items = await listPublicPaymentProducts();
      if (requestToken !== paymentProductsRequestTokenRef.current) return;
      setPaymentProducts(items);
      setPaymentProductsError('');
    } catch (loadError) {
      if (requestToken !== paymentProductsRequestTokenRef.current) return;
      setPaymentProducts([]);
      setPaymentProductsError(readableErrorMessage(loadError, '无法加载 LDC 兑换项目。'));
    } finally {
      if (requestToken === paymentProductsRequestTokenRef.current) setPaymentProductsLoading(false);
    }
  }

  async function loadPaymentOrders(): Promise<void> {
    const requestToken = ++paymentOrdersRequestTokenRef.current;
    try {
      setPaymentOrdersLoading(true);
      const items = await listMyPaymentOrders();
      if (requestToken !== paymentOrdersRequestTokenRef.current) return;
      setPaymentOrders(items);
      setPaymentOrdersError('');
      const pendingOrder = items.find((item) => item.status === 'created' || item.status === 'pending' || (item.status === 'paid' && !item.applied_at));
      setPollingOrderNo(pendingOrder?.out_trade_no ?? '');
    } catch (loadError) {
      if (requestToken !== paymentOrdersRequestTokenRef.current) return;
      setPaymentOrders([]);
      setPaymentOrdersError(readableErrorMessage(loadError, '无法加载 LDC 订单记录。'));
    } finally {
      if (requestToken === paymentOrdersRequestTokenRef.current) setPaymentOrdersLoading(false);
    }
  }

  async function refreshOnePaymentOrder(outTradeNo: string, silent = false): Promise<void> {
    if (!outTradeNo || !authenticated || !csrfToken) return;
    try {
      const order = await refreshMyPaymentOrder(outTradeNo, csrfToken);
      setPaymentOrders((current) => upsertPaymentOrder(current, order));
      if (order.status === 'paid' && order.applied_at) {
        setPollingOrderNo('');
        clearRememberedPaymentOrder(order.out_trade_no);
        if (!silent) {
          setPaymentNotice({ tone: 'success', message: `订单 ${order.out_trade_no} 已支付成功，权益已经发放。` });
        }
      } else if (order.status === 'failed' || order.status === 'refunded') {
        setPollingOrderNo('');
        clearRememberedPaymentOrder(order.out_trade_no);
      }
    } catch (refreshError) {
      if (!silent) {
        setPaymentNotice({ tone: 'error', message: readableErrorMessage(refreshError, '刷新订单状态失败。') });
      }
    }
  }

  async function handleCreatePaymentOrder(product: PaymentProduct): Promise<void> {
    if (!authenticated) {
      onLogin();
      return;
    }
    if (!csrfToken) {
      setPaymentNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const units = Math.max(1, Math.floor(paymentUnits[product.key] ?? 1));
    try {
      setCreatingProductKey(product.key);
      setPaymentNotice(null);
      const order = await createMyPaymentOrder({ product_key: product.key, units }, csrfToken);
      setPaymentOrders((current) => upsertPaymentOrder(current, order));
      setPollingOrderNo(order.out_trade_no);
      rememberLatestPaymentOrder(order.out_trade_no);

      const openedWindow = openTrustedPaymentWindow(order.payment_url);
      if (!openedWindow) {
        setPaymentNotice({
          tone: 'info',
          message: `订单 ${order.out_trade_no} 已创建，但浏览器拦截了新窗口，或者支付链接不是受信任的 Linux Do Credit 地址。`,
        });
        return;
      }

      setPaymentNotice({
        tone: 'success',
        message: `订单 ${order.out_trade_no} 已创建，新标签页已经打开支付页面。当前页面会自动轮询支付状态。`,
      });
    } catch (createError) {
      setPaymentNotice({ tone: 'error', message: readableErrorMessage(createError, '创建 LDC 订单失败。') });
    } finally {
      setCreatingProductKey('');
    }
  }

  function openLiveEntry(): void {
    if (!authenticated) {
      onLogin();
      return;
    }
    if (selectedItem?.key === emailCatchAllPermissionKey) onOpenEmails();
  }

  if (!selectedItem) return null;

  return viewMode === 'records' ? (
    <RecordsView
      authenticated={authenticated}
      loading={loading || sessionLoading}
      error={error}
      rows={rows}
      applicationRows={applicationRows}
      onBack={() => setViewMode('main')}
      onLogin={onLogin}
    />
  ) : (
    <div className="mx-auto max-w-5xl px-6 pb-24 pt-32">
      <motion.div initial={{ y: 20, opacity: 0 }} animate={{ y: 0, opacity: 1 }} className="mb-12 text-center">
        <div className="inline-flex items-center justify-center rounded-full bg-teal-100 p-3 text-teal-600 dark:bg-teal-900/30 dark:text-teal-400">
          <ShieldPlus size={32} />
        </div>
        <h1 className="mt-4 text-3xl font-extrabold text-gray-900 dark:text-white md:text-4xl">权限申请与兑换</h1>
        <p className="mx-auto mt-4 max-w-3xl text-lg leading-8 text-gray-700 dark:text-gray-300">
          恢复原本的多权限入口设计。当前真实接入的是邮箱泛解析权限，其他入口继续保留，便于后续在不破坏 UI 的前提下逐步上线。
        </p>
        <button type="button" onClick={() => setViewMode('records')} className="mt-6 inline-flex items-center gap-2 rounded-full border border-teal-200 bg-white/50 px-5 py-2.5 font-medium text-teal-700 shadow-sm transition hover:bg-teal-50 dark:border-teal-900/50 dark:bg-black/40 dark:text-teal-300 dark:hover:bg-teal-900/25">
          <List size={18} />
          查看我的权限记录
        </button>
      </motion.div>

      {error ? <div className="mb-6 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">{error}</div> : null}
      {(loading || sessionLoading) ? <div className="mb-6 rounded-2xl border border-slate-200/70 bg-white/60 px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-white/10 dark:bg-white/5 dark:text-slate-300">正在同步你的权限状态...</div> : null}
      {!authenticated ? <div className="mb-6 rounded-2xl border border-amber-300/40 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-950/25 dark:text-amber-200">当前未登录。你仍可浏览完整权限结构，但真实状态、申请记录和可用入口需要登录后才能读取。</div> : null}

      <div className="grid grid-cols-1 gap-8 lg:grid-cols-5">
        <div className="lg:col-span-2">
          <GlassCard className="h-full">
            <div className="mb-6 flex items-center gap-3">
              <div className="rounded-xl bg-teal-100 p-2 text-teal-600 dark:bg-teal-900/50 dark:text-teal-400"><Ticket size={24} /></div>
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">兑换码</h2>
            </div>
            <p className="mb-6 text-sm leading-7 text-gray-600 dark:text-gray-400">兑换区位已经恢复，但当前后端没有兑换码核销接口，因此这里只保留输入位和原设计布局，不会出现假成功。</p>
            <form className="space-y-4" onSubmit={(event) => event.preventDefault()}>
              <input type="text" value={redeemCode} onChange={(event) => setRedeemCode(event.target.value)} placeholder="输入兑换码（当前仅保留界面）" className="w-full rounded-xl border border-gray-200 bg-white/50 px-4 py-3 font-mono text-gray-900 outline-none transition focus:ring-2 focus:ring-teal-500 dark:border-gray-700 dark:bg-black/50 dark:text-white" />
              <button type="submit" disabled className="flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-teal-500 to-emerald-600 px-6 py-3 font-medium text-white opacity-60">
                <Key size={18} />
                兑换功能暂未开放
              </button>
            </form>
            <div className="mt-5 rounded-2xl border border-amber-300/35 bg-amber-50/80 px-4 py-4 text-sm leading-7 text-amber-900 dark:border-amber-500/20 dark:bg-amber-950/25 dark:text-amber-100">当前兑换码记录不会被伪造写入，真正开放前这里只负责恢复页面完整结构。</div>
          </GlassCard>
        </div>

        <div className="lg:col-span-3">
          <GlassCard>
            <div className="mb-6 flex items-center gap-3">
              <div className="rounded-xl bg-emerald-100 p-2 text-emerald-600 dark:bg-emerald-900/50 dark:text-emerald-400"><Send size={24} /></div>
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">申请高级权限</h2>
            </div>
            <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">申请权限类型</label>
                <GlassSelect options={options} value={selectedItem.key} onChange={setSelectedKey} />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">{selectedItem.stage === 'live' ? '目标权限对象' : '目标域名 / 前缀'}</label>
                <input type="text" value={selectedItem.stage === 'live' ? selectedTarget : plannedTarget} readOnly={selectedItem.stage === 'live'} onChange={(event) => setPlannedTarget(event.target.value)} className="w-full rounded-xl border border-gray-200 bg-white/50 px-4 py-3 text-gray-900 outline-none transition focus:ring-2 focus:ring-teal-500 dark:border-gray-700 dark:bg-black/50 dark:text-white" />
              </div>
            </div>
            <div className="mt-5 flex flex-wrap items-center gap-3">
              <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${selectedStatus.className}`}>{selectedStatus.label}</span>
              <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${stageBadge.className}`}>{stageBadge.label}</span>
            </div>
            <div className="mt-5 rounded-3xl border border-white/20 bg-white/35 p-5 dark:border-white/10 dark:bg-black/20">
              <div className="text-lg font-bold text-gray-900 dark:text-white">{selectedItem.displayName}</div>
              <p className="mt-3 text-sm leading-7 text-gray-600 dark:text-gray-300">{selectedItem.description}</p>
              <p className="mt-3 text-sm leading-7 text-gray-600 dark:text-gray-300">{selectedItem.hint}</p>
            </div>
            {selectedItem.stage === 'live' ? (
              <>
                <div className="mt-6 grid gap-4 md:grid-cols-2">
                  <StatCard title="当前状态" value={selectedStatus.label} />
                  <StatCard title="目标对象" value={selectedTarget} mono />
                  <StatCard title="最低等级要求" value={`TL ${selectedPermission?.min_trust_level ?? 2}`} />
                  <StatCard title="当前账号" value={authenticated ? user?.username ?? '-' : '未登录'} />
                </div>

                {selectedPermission?.eligibility_reasons?.length ? (
                  <div className="mt-6 rounded-2xl border border-amber-300/40 bg-amber-50/80 p-4 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-950/25 dark:text-amber-200">
                    <div className="mb-2 flex items-center gap-2 font-semibold">
                      <ShieldAlert size={16} />
                      当前不可直接申请
                    </div>
                    <div className="space-y-2 leading-6">
                      {selectedPermission.eligibility_reasons.map((reason) => (
                        <div key={reason}>- {formatEligibilityReason(reason, selectedPermission)}</div>
                      ))}
                    </div>
                  </div>
                ) : null}

                {selectedPermission?.application ? (
                  <div className="mt-6 grid gap-4 md:grid-cols-2">
                    <StatCard title="最近提交" value={formatDate(selectedPermission.application.created_at)} />
                    <StatCard title="审核备注" value={selectedPermission.application.review_note || '暂无审核备注'} />
                  </div>
                ) : null}

                <div className="mt-6 flex flex-wrap gap-3">
                  <button type="button" onClick={openLiveEntry} className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-teal-600 px-6 py-3 font-medium text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700">
                    <ArrowRight size={18} />
                    {buildLiveEntryButtonLabel(authenticated, selectedPermission, selectedItem.key)}
                  </button>
                </div>
              </>
            ) : (
              <>
                <div className="mt-6">
                  <div className="mb-2 flex items-end justify-between">
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">申请原因（保留旧设计，不会提交）</label>
                    <span className={`text-xs font-mono ${plannedReason.length >= 30 ? 'text-teal-600 dark:text-teal-400' : 'text-red-500'}`}>{plannedReason.length} / 30</span>
                  </div>
                  <textarea value={plannedReason} onChange={(event) => setPlannedReason(event.target.value)} rows={4} placeholder="请在这里描述用途、项目背景和申请原因。当前仅恢复 UI 结构，后端尚未开放。" className="w-full resize-none rounded-xl border border-gray-200 bg-white/50 px-4 py-3 text-gray-900 outline-none transition focus:ring-2 focus:ring-teal-500 dark:border-gray-700 dark:bg-black/50 dark:text-white" />
                </div>
                <div className="mt-6 rounded-2xl border border-sky-300/35 bg-sky-50/80 px-4 py-4 text-sm leading-7 text-sky-900 dark:border-sky-500/20 dark:bg-sky-950/20 dark:text-sky-100">当前权限位只恢复了设计结构与填写区域，实际提交通道会在后续版本对接完成后再开放。这样既保留旧设计，也不会误导用户以为已经可以申请。</div>
                <div className="mt-6">
                  <button type="button" disabled className="flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-emerald-500 to-teal-600 px-6 py-3 font-medium text-white opacity-60">
                    <Send size={18} />
                    当前权限暂未开放申请
                  </button>
                </div>
              </>
            )}
          </GlassCard>
        </div>
      </div>

      <PaymentExchangeSection
        authenticated={authenticated}
        loading={paymentProductsLoading || paymentOrdersLoading}
        products={paymentProducts}
        productsError={paymentProductsError}
        orders={paymentOrders}
        ordersError={paymentOrdersError}
        units={paymentUnits}
        creatingProductKey={creatingProductKey}
        notice={paymentNotice}
        onLogin={onLogin}
        onChangeUnits={(productKey, value) =>
          setPaymentUnits((current) => ({
            ...current,
            [productKey]: Math.max(1, Math.floor(value) || 1),
          }))
        }
        onCreateOrder={(product) => void handleCreatePaymentOrder(product)}
        onRefreshOrder={(outTradeNo) => void refreshOnePaymentOrder(outTradeNo)}
      />

      <POWBenefitSection
        authenticated={authenticated}
        csrfToken={csrfToken}
        catchAllPermission={permissionMap.get(emailCatchAllPermissionKey) ?? null}
        onLogin={onLogin}
        onRewardClaimed={loadPermissions}
      />
    </div>
  );
}

interface PaymentExchangeSectionProps {
  authenticated: boolean;
  loading: boolean;
  products: PaymentProduct[];
  productsError: string;
  orders: PaymentOrder[];
  ordersError: string;
  units: Record<string, number>;
  creatingProductKey: string;
  notice: PaymentNotice | null;
  onLogin: () => void;
  onChangeUnits: (productKey: string, value: number) => void;
  onCreateOrder: (product: PaymentProduct) => void;
  onRefreshOrder: (outTradeNo: string) => void;
}

function PaymentExchangeSection({
  authenticated,
  loading,
  products,
  productsError,
  orders,
  ordersError,
  units,
  creatingProductKey,
  notice,
  onLogin,
  onChangeUnits,
  onCreateOrder,
  onRefreshOrder,
}: PaymentExchangeSectionProps) {
  const [selectedProductKey, setSelectedProductKey] = useState('');
  const [ordersOpen, setOrdersOpen] = useState(false);

  useEffect(() => {
    if (products.length === 0) {
      setSelectedProductKey('');
      return;
    }
    if (!products.some((item) => item.key === selectedProductKey)) {
      setSelectedProductKey(products[0].key);
    }
  }, [products, selectedProductKey]);

  const productOptions = useMemo<GlassSelectOption[]>(
    () => products.map((item) => ({ value: item.key, label: item.display_name })),
    [products],
  );
  const selectedProduct = useMemo(
    () => products.find((item) => item.key === selectedProductKey) ?? null,
    [products, selectedProductKey],
  );
  const selectedQuantity = selectedProduct ? Math.max(1, units[selectedProduct.key] ?? 1) : 1;
  const selectedTotalPriceCents = selectedProduct ? selectedProduct.unit_price_cents * selectedQuantity : 0;
  const selectedTotalGrant = selectedProduct ? selectedProduct.grant_quantity * selectedQuantity : 0;
  const creating = selectedProduct ? creatingProductKey === selectedProduct.key : false;
  const pendingOrderCount = orders.filter((order) => isOrderWaitingForRefresh(order)).length;
  const selectedProductBehavior = selectedProduct ? describePaymentProductBehavior(selectedProduct) : null;

  return (
    <div className="mt-10 space-y-6">
      <motion.div initial={{ y: 20, opacity: 0 }} animate={{ y: 0, opacity: 1 }}>
        <GlassCard>
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <div className="inline-flex items-center gap-2 rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                <CreditCard size={14} />
                Linux Do Credit 兑换
              </div>
              <h2 className="mt-4 text-2xl font-bold text-gray-900 dark:text-white">使用 LDC 兑换邮箱权益与测试项目</h2>
              <p className="mt-3 max-w-3xl text-sm leading-7 text-gray-600 dark:text-gray-300">
                当前兑换区已经接入真实后端。你可以直接创建 LDC 订单，系统会在支付完成后自动轮询并刷新本地权益状态。邮箱泛解析相关项目只负责增加订阅或额度，本身不会替代权限审核流程。
              </p>
            </div>
            <div className="flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={() => {
                  if (!authenticated) {
                    onLogin();
                    return;
                  }
                  setOrdersOpen(true);
                }}
                  className="inline-flex min-w-[12rem] items-center justify-center gap-2 whitespace-nowrap rounded-2xl border border-emerald-200 bg-white/70 px-6 py-3 text-sm font-semibold text-emerald-700 shadow-sm transition hover:bg-emerald-50 dark:border-emerald-900/40 dark:bg-black/30 dark:text-emerald-300 dark:hover:bg-emerald-900/20"
                >
                  <Ticket size={16} />
                  查看全部订单
                {authenticated && orders.length > 0 ? (
                  <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs font-bold text-emerald-700 dark:text-emerald-300">
                    {pendingOrderCount > 0 ? `${pendingOrderCount} 待处理 / ${orders.length}` : `${orders.length} 条`}
                  </span>
                ) : null}
              </button>
              {!authenticated ? (
                <button
                  type="button"
                  onClick={onLogin}
                  className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-5 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700"
                >
                  <ArrowRight size={16} />
                  登录后兑换
                </button>
              ) : null}
            </div>
          </div>
        </GlassCard>
      </motion.div>

      {productsError ? <InlineNotice tone="error" message={productsError} /> : null}
      {ordersError ? <InlineNotice tone="error" message={ordersError} /> : null}
      {notice ? <InlineNotice tone={notice.tone} message={notice.message} /> : null}
      {loading ? <InlineNotice tone="info" message="正在同步 LDC 商品与订单状态..." /> : null}

      <GlassCard>
        {!loading && products.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/25 bg-white/25 px-5 py-8 text-sm leading-7 text-gray-600 dark:border-white/10 dark:bg-black/15 dark:text-gray-300">
            当前没有可展示的 LDC 兑换项目。管理员可以先在后台启用或调整商品配置。
          </div>
        ) : (
          <div className="space-y-6">
            <div className="grid gap-5 lg:grid-cols-[minmax(0,1.1fr)_minmax(320px,0.9fr)]">
              <div className="space-y-5">
                <div>
                  <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">选择兑换项目</label>
                  <GlassSelect
                    options={productOptions}
                    value={selectedProductKey}
                    onChange={setSelectedProductKey}
                    placeholder={loading ? '正在加载兑换项目...' : '请选择一个兑换项目'}
                    disabled={loading || products.length === 0}
                  />
                </div>

                {selectedProduct ? (
                  <div className="rounded-[1.75rem] border border-white/20 bg-white/35 p-6 dark:border-white/10 dark:bg-black/20">
                    <div className="flex flex-wrap items-center gap-3">
                      <div className="rounded-2xl bg-emerald-100 px-3 py-1 text-sm font-semibold text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                        {selectedProduct.display_name}
                      </div>
                      <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${selectedProduct.enabled ? 'bg-sky-100 text-sky-700 dark:bg-sky-900/25 dark:text-sky-300' : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                        {selectedProduct.enabled ? '可兑换' : '已关闭'}
                      </span>
                    </div>

                    <div className="mt-4 text-xl font-bold text-gray-900 dark:text-white">
                      {selectedProductBehavior?.headline ?? selectedProduct.display_name}
                    </div>
                    <p className="mt-3 text-sm leading-7 text-gray-600 dark:text-gray-300">{selectedProduct.description}</p>
                    {selectedProductBehavior?.detail ? (
                      <p className="mt-3 text-sm leading-7 text-gray-600 dark:text-gray-300">{selectedProductBehavior.detail}</p>
                    ) : null}

                    <div className="mt-5 grid gap-3 md:grid-cols-3">
                      <StatCard title="单价" value={`${formatLDC(selectedProduct.unit_price_cents)} LDC`} />
                      <StatCard title="单份权益" value={formatGrantAmount(selectedProduct.grant_quantity, selectedProduct.grant_unit)} />
                      <StatCard title="总权益" value={formatGrantAmount(selectedTotalGrant, selectedProduct.grant_unit)} />
                    </div>
                  </div>
                ) : null}
              </div>

              <div className="rounded-[1.75rem] border border-white/20 bg-white/35 p-6 dark:border-white/10 dark:bg-black/20">
                <div className="text-sm font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">本次操作</div>
                <div className="mt-3 text-lg font-bold text-gray-900 dark:text-white">
                  {selectedProductBehavior?.actionTitle ?? '请选择一个兑换项目'}
                </div>
                <div className="mt-3 text-sm leading-7 text-gray-600 dark:text-gray-300">
                  {selectedProductBehavior?.actionDescription ?? '选择项目后，这里会显示对应的兑换内容、金额和操作入口。'}
                </div>

                <div className="mt-5">
                  <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
                    {selectedProductBehavior?.quantityLabel ?? '兑换数量'}
                  </label>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={selectedQuantity}
                    disabled={!selectedProduct}
                    onChange={(event) => {
                      if (!selectedProduct) return;
                      onChangeUnits(selectedProduct.key, Number(event.target.value));
                    }}
                    className="w-full rounded-2xl border border-gray-200 bg-white/70 px-4 py-3 text-gray-900 outline-none transition focus:ring-2 focus:ring-emerald-500 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-700 dark:bg-black/40 dark:text-white"
                  />
                </div>

                <div className="mt-4 rounded-2xl border border-white/15 bg-white/45 px-4 py-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/25 dark:text-gray-200">
                  <div>
                    本次合计：
                    <span className="ml-2 font-semibold text-gray-900 dark:text-white">{formatLDC(selectedTotalPriceCents)} LDC</span>
                  </div>
                  {selectedProductBehavior?.totalSummary ? (
                    <div className="mt-2">{selectedProductBehavior.totalSummary(selectedTotalGrant)}</div>
                  ) : null}
                </div>

                <button
                  type="button"
                  disabled={!selectedProduct || !selectedProduct.enabled || creating}
                  onClick={() => {
                    if (!selectedProduct) return;
                    onCreateOrder(selectedProduct);
                  }}
                  className="mt-5 inline-flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-5 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {creating ? <LoaderCircle size={16} className="animate-spin" /> : <CreditCard size={16} />}
                  {!selectedProduct
                    ? '请选择项目'
                    : authenticated
                      ? (creating ? '创建订单中...' : (selectedProductBehavior?.actionButtonLabel ?? '立即创建订单'))
                      : '登录后兑换'}
                </button>

                <div className="mt-3 text-xs leading-6 text-gray-500 dark:text-gray-400">
                  创建成功后会在新标签页打开支付页。订单详情已经移到“查看全部订单”，避免当前页面继续被订单表挤压。
                </div>
              </div>
            </div>
          </div>
        )}
      </GlassCard>

      <AnimatePresence>
        {ordersOpen ? (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-6 backdrop-blur-sm"
            onClick={() => setOrdersOpen(false)}
          >
            <motion.div
              initial={{ opacity: 0, y: 20, scale: 0.98 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 20, scale: 0.98 }}
              transition={{ duration: 0.2 }}
              className="w-full max-w-5xl rounded-[2rem] border border-white/20 bg-white/85 shadow-2xl backdrop-blur-2xl dark:border-white/10 dark:bg-slate-950/88"
              onClick={(event) => event.stopPropagation()}
            >
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/15 px-6 py-5 dark:border-white/10">
                <div>
                  <div className="flex items-center gap-2 text-lg font-bold text-gray-900 dark:text-white">
                    <Ticket size={18} className="text-emerald-500" />
                    全部订单
                  </div>
                  <div className="mt-1 text-sm text-gray-600 dark:text-gray-300">待支付订单仍然支持手动刷新，支付页也可以从这里重新打开。</div>
                </div>
                <button
                  type="button"
                  onClick={() => setOrdersOpen(false)}
                  className="rounded-2xl border border-white/20 bg-white/70 px-4 py-2 text-sm font-medium text-gray-900 transition hover:bg-white dark:border-white/10 dark:bg-black/35 dark:text-white dark:hover:bg-black/50"
                >
                  关闭
                </button>
              </div>

              <div className="max-h-[70vh] overflow-auto">
                <table className="min-w-full border-collapse text-left">
                  <thead>
                    <tr className="border-b border-white/10 bg-white/10 dark:border-white/5 dark:bg-black/10">
                      <th className="px-5 py-4 text-sm font-semibold text-gray-900 dark:text-white">项目</th>
                      <th className="px-5 py-4 text-sm font-semibold text-gray-900 dark:text-white">金额</th>
                      <th className="px-5 py-4 text-sm font-semibold text-gray-900 dark:text-white">状态</th>
                      <th className="px-5 py-4 text-sm font-semibold text-gray-900 dark:text-white">时间</th>
                      <th className="px-5 py-4 text-right text-sm font-semibold text-gray-900 dark:text-white">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {orders.map((order) => {
                      const waiting = isOrderWaitingForRefresh(order);
                      const status = describePaymentOrderStatus(order);
                      return (
                        <tr key={order.out_trade_no} className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                          <td className="px-5 py-4">
                            <div className="font-semibold text-gray-900 dark:text-white">{order.product_name}</div>
                            <div className="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400">{order.out_trade_no}</div>
                          </td>
                          <td className="px-5 py-4 text-gray-700 dark:text-gray-200">{formatLDC(order.total_price_cents)} LDC</td>
                          <td className="px-5 py-4">
                            <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${status.className}`}>{status.label}</span>
                          </td>
                          <td className="px-5 py-4 text-gray-600 dark:text-gray-300">
                            <div>{formatDate(order.created_at)}</div>
                            <div className="mt-1 text-xs text-gray-500 dark:text-gray-400">{order.applied_at ? `发放 ${formatDate(order.applied_at)}` : order.paid_at ? `支付 ${formatDate(order.paid_at)}` : '尚未完成支付'}</div>
                          </td>
                          <td className="px-5 py-4">
                            <div className="flex justify-end gap-2">
                              {order.payment_url ? (
                                <button
                                  type="button"
                                  onClick={() => openTrustedPaymentWindow(order.payment_url)}
                                  className="rounded-xl p-2 text-emerald-500 transition hover:bg-emerald-100 dark:hover:bg-emerald-900/25"
                                  aria-label={`打开订单 ${order.out_trade_no} 的支付页`}
                                >
                                  <ExternalLink size={16} />
                                </button>
                              ) : null}
                              {waiting ? (
                                <button
                                  type="button"
                                  onClick={() => onRefreshOrder(order.out_trade_no)}
                                  className="rounded-xl px-3 py-2 text-xs font-semibold text-sky-700 transition hover:bg-sky-100 dark:text-sky-300 dark:hover:bg-sky-900/25"
                                >
                                  刷新状态
                                </button>
                              ) : null}
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                    {!loading && orders.length === 0 ? (
                      <tr>
                        <td colSpan={5} className="px-5 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
                          {authenticated ? '当前还没有任何 LDC 订单。' : '登录后可查看你自己的 LDC 订单记录。'}
                        </td>
                      </tr>
                    ) : null}
                  </tbody>
                </table>
              </div>
            </motion.div>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}

interface RecordsViewProps {
  authenticated: boolean;
  loading: boolean;
  error: string;
  rows: OverviewRow[];
  applicationRows: Array<{ row: OverviewRow; application: NonNullable<UserPermission['application']> }>;
  onBack: () => void;
  onLogin: () => void;
}

function RecordsView({ authenticated, loading, error, rows, applicationRows, onBack, onLogin }: RecordsViewProps) {
  return (
    <div className="mx-auto max-w-5xl px-6 pb-24 pt-32">
      <motion.div initial={{ y: 20, opacity: 0 }} animate={{ y: 0, opacity: 1 }} className="mb-8">
        <button type="button" onClick={onBack} className="mb-6 flex items-center gap-2 font-medium text-teal-600 transition-colors hover:text-teal-700 dark:text-teal-400 dark:hover:text-teal-300">
          <ArrowLeft size={20} />
          返回申请页
        </button>
        <div className="flex items-center gap-3">
          <div className="rounded-xl bg-teal-100 p-3 text-teal-600 dark:bg-teal-900/30 dark:text-teal-400"><List size={28} /></div>
          <div>
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">我的权限记录</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400">查看当前所有权限入口的实际状态、申请记录与保留的兑换区位。</p>
          </div>
        </div>
      </motion.div>

      {error ? <div className="mb-6 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">{error}</div> : null}

      {!authenticated ? (
        <GlassCard className="text-center">
          <div className="mb-4 inline-flex items-center justify-center rounded-full bg-white/60 p-3 text-emerald-600 dark:bg-white/10 dark:text-emerald-300"><ShieldAlert size={28} /></div>
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">登录后查看真实权限记录</h2>
          <p className="mx-auto mt-4 max-w-2xl text-sm leading-7 text-gray-600 dark:text-gray-300">当前页面已经恢复了完整结构，但个人权限状态、申请记录和审核备注都与 Linux Do OAuth 身份绑定，未登录时不会显示这些真实数据。</p>
          <button type="button" onClick={onLogin} className="mt-6 inline-flex items-center gap-2 rounded-2xl bg-[#1a1a1a] px-6 py-3 font-bold text-white shadow-lg transition-all hover:bg-black dark:bg-white dark:text-black dark:hover:bg-gray-100">
            <ArrowRight size={18} />
            使用 Linux Do 登录
          </button>
        </GlassCard>
      ) : (
        <div className="space-y-8">
          <SectionTitle icon={<Key size={20} className="text-teal-500" />} title="权限总览" />
          <GlassCard className="overflow-hidden p-0">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[760px] border-collapse text-left">
                <thead>
                  <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-black/20">
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">权限类型</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">目标对象</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">当前状态</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">接入状态</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">最近变更</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? <tr><td colSpan={5} className="p-8 text-center text-sm text-gray-500 dark:text-gray-400">正在加载权限记录...</td></tr> : null}
                  {!loading ? rows.map(({ item, target, permission }) => {
                    const status = describeEntryStatus(item, permission);
                    const stage = describeStage(item.stage);
                    return (
                      <tr key={item.key} className="border-b border-white/10 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                        <td className="p-4 font-medium text-gray-900 dark:text-white">{item.typeLabel}</td>
                        <td className="p-4 font-mono text-sm text-teal-600 dark:text-teal-300">{target}</td>
                        <td className="p-4"><span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${status.className}`}>{status.label}</span></td>
                        <td className="p-4"><span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${stage.className}`}>{stage.label}</span></td>
                        <td className="p-4 text-sm text-gray-600 dark:text-gray-400">{permission?.application?.updated_at ? formatDate(permission.application.updated_at) : '暂无'}</td>
                      </tr>
                    );
                  }) : null}
                </tbody>
              </table>
            </div>
          </GlassCard>
          <SectionTitle icon={<Send size={20} className="text-emerald-500" />} title="权限申请记录" />
          <GlassCard className="overflow-hidden p-0">
            <div className="overflow-x-auto">
              <table className="w-full min-w-[820px] border-collapse text-left">
                <thead>
                  <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-black/20">
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">权限类型</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">目标对象</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">状态</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">申请时间</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">最近变更</th>
                    <th className="p-4 text-sm font-semibold text-gray-900 dark:text-white">审核备注</th>
                  </tr>
                </thead>
                <tbody>
                  {loading ? <tr><td colSpan={6} className="p-8 text-center text-sm text-gray-500 dark:text-gray-400">正在加载申请记录...</td></tr> : null}
                  {!loading && applicationRows.length === 0 ? <tr><td colSpan={6} className="p-8 text-center text-sm text-gray-500 dark:text-gray-400">当前还没有任何已提交的权限申请记录。</td></tr> : null}
                  {!loading ? applicationRows.map(({ row, application }) => {
                    const status = describePermissionStatus(application.status);
                    return (
                      <tr key={`${row.item.key}-${application.id}`} className="border-b border-white/10 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                        <td className="p-4 font-medium text-gray-900 dark:text-white">{row.item.typeLabel}</td>
                        <td className="p-4 font-mono text-sm text-emerald-600 dark:text-emerald-300">{row.target}</td>
                        <td className="p-4">
                          <span className={`inline-flex items-center gap-1 rounded-full px-3 py-1 text-xs font-semibold ${status.className}`}>
                            {application.status === 'approved' ? <CheckCircle2 size={12} /> : application.status === 'rejected' ? <XCircle size={12} /> : <Clock3 size={12} />}
                            {status.label}
                          </span>
                        </td>
                        <td className="p-4 text-sm text-gray-600 dark:text-gray-400">{formatDate(application.created_at)}</td>
                        <td className="p-4 text-sm text-gray-600 dark:text-gray-400">{formatDate(application.updated_at)}</td>
                        <td className="p-4 text-sm text-gray-700 dark:text-gray-200">{application.review_note || '暂无审核备注'}</td>
                      </tr>
                    );
                  }) : null}
                </tbody>
              </table>
            </div>
          </GlassCard>
          <SectionTitle icon={<Ticket size={20} className="text-amber-500" />} title="兑换码记录" />
          <GlassCard>
            <div className="rounded-2xl border border-dashed border-white/25 bg-white/25 px-5 py-8 text-sm leading-7 text-gray-600 dark:border-white/10 dark:bg-black/15 dark:text-gray-300">当前兑换码体系仍未接入真实核销接口，因此没有可展示的兑换记录。此区域保留是为了恢复之前的完整页面结构，而不是移除入口。</div>
          </GlassCard>
        </div>
      )}
    </div>
  );
}

function SectionTitle({ icon, title }: { icon: ReactNode; title: string }) {
  return <h2 className="mb-4 flex items-center gap-2 text-xl font-bold text-gray-900 dark:text-white">{icon}{title}</h2>;
}

function StatCard({ title, value, mono = false }: { title: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
      <div className="text-xs font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className={`mt-2 font-semibold text-gray-900 dark:text-white ${mono ? 'font-mono text-sm' : 'text-base'}`}>{value}</div>
    </div>
  );
}

function describeEntryStatus(item: CatalogItem, permission: UserPermission | null) {
  if (item.stage === 'planned') return { label: '暂未开放', className: 'bg-sky-100 text-sky-700 dark:bg-sky-900/25 dark:text-sky-300' };
  return describePermissionStatus(permission?.status ?? 'not_requested');
}

function describeStage(stage: EntryStage) {
  return stage === 'live'
    ? { label: '已接入', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' }
    : { label: '规划中', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
}

function describePermissionStatus(status: UserPermission['status']) {
  switch (status) {
    case 'approved':
      return { label: '已通过', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
    case 'pending':
      return { label: '待审核', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300' };
    case 'rejected':
      return { label: '未通过', className: 'bg-red-100 text-red-700 dark:bg-red-900/25 dark:text-red-300' };
    default:
      return { label: '尚未申请', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
}

function describePaymentOrderStatus(order: PaymentOrder) {
  if (order.status === 'paid' && order.applied_at) {
    return { label: '已支付并发放', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' };
  }
  switch (order.status) {
    case 'paid':
      return { label: '已支付，待发放', className: 'bg-sky-100 text-sky-700 dark:bg-sky-900/25 dark:text-sky-300' };
    case 'pending':
      return { label: '待支付', className: 'bg-amber-100 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300' };
    case 'failed':
      return { label: '创建失败', className: 'bg-red-100 text-red-700 dark:bg-red-900/25 dark:text-red-300' };
    case 'refunded':
      return { label: '已退款', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
    default:
      return { label: '已创建', className: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' };
  }
}

function buildLiveEntryButtonLabel(authenticated: boolean, permission: UserPermission | null, key: string): string {
  if (!authenticated) return '登录后申请';
  if (permission?.can_manage_route) return '前往邮箱页面管理';
  if (permission?.can_apply) return '前往邮箱页面申请';
  return '前往邮箱页面查看详情';
}

function InlineNotice({ tone, message }: { tone: NoticeTone; message: string }) {
  const palette = tone === 'success'
    ? 'border-emerald-300/35 bg-emerald-100/70 text-emerald-900 dark:border-emerald-700/35 dark:bg-emerald-950/30 dark:text-emerald-100'
    : tone === 'error'
      ? 'border-red-300/35 bg-red-100/70 text-red-900 dark:border-red-700/35 dark:bg-red-950/30 dark:text-red-100'
      : 'border-sky-300/35 bg-sky-100/70 text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/30 dark:text-sky-100';

  return <div className={`rounded-2xl border px-4 py-3 text-sm leading-7 ${palette}`}>{message}</div>;
}

function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message;
  if (error instanceof Error && error.message.trim() !== '') return error.message;
  return fallback;
}

function formatEligibilityReason(reason: string, permission: UserPermission): string {
  switch (reason) {
    case 'trust_level_below_minimum':
      return `当前信任等级不足，需要至少达到 TL ${permission.min_trust_level}`;
    case 'policy_disabled':
      return '当前管理员已关闭该权限策略';
    case 'already_has_permission':
      return '你已经拥有该权限';
    case 'already_has_pending_application':
      return '你已经提交过申请，请等待审核';
    default:
      return reason;
  }
}

function formatDate(value?: string): string {
  if (!value) return '暂无';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
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

function formatGrantAmount(value: number, unit: string): string {
  switch (unit) {
    case 'day':
      return `${value.toLocaleString('zh-CN')} 天`;
    case 'message':
      return `${value.toLocaleString('zh-CN')} 条`;
    case 'run':
      return `${value.toLocaleString('zh-CN')} 次`;
    default:
      return `${value.toLocaleString('zh-CN')} ${unit}`;
  }
}

function upsertPaymentOrder(orders: PaymentOrder[], nextOrder: PaymentOrder): PaymentOrder[] {
  const existingIndex = orders.findIndex((item) => item.out_trade_no === nextOrder.out_trade_no);
  if (existingIndex >= 0) {
    return orders.map((item, index) => (index === existingIndex ? nextOrder : item));
  }
  return [nextOrder, ...orders].sort((left, right) => new Date(right.created_at).getTime() - new Date(left.created_at).getTime());
}

function openTrustedPaymentWindow(rawURL: string): Window | null {
  try {
    const parsedURL = new URL(rawURL);
    if (parsedURL.protocol !== 'https:' || parsedURL.hostname !== 'credit.linux.do') {
      return null;
    }
    return window.open(parsedURL.toString(), '_blank', 'noopener,noreferrer');
  } catch {
    return null;
  }
}

function isOrderWaitingForRefresh(order: PaymentOrder): boolean {
  return order.status === 'created' || order.status === 'pending' || (order.status === 'paid' && !order.applied_at);
}

function describePaymentProductBehavior(product: PaymentProduct) {
  switch (product.effect_type) {
    case 'email_catch_all_subscription_days':
      return {
        headline: '为邮箱泛解析增加订阅时长',
        detail: '该项目会直接顺延邮箱泛解析的订阅到期时间。只有已经通过邮箱泛解析权限审核的用户才能实际使用。',
        quantityLabel: '购买份数',
        actionTitle: '创建订阅订单',
        actionDescription: '每份都会按当前配置增加固定订阅天数。支付完成后，后端会自动发放并刷新状态。',
        actionButtonLabel: '创建订阅订单',
        totalSummary: (totalGrant: number) => `本次将增加 ${formatGrantAmount(totalGrant, product.grant_unit)} 的订阅时长。`,
      };
    case 'email_catch_all_remaining_count':
      return {
        headline: '为邮箱泛解析增加可用额度',
        detail: '该项目会增加邮箱泛解析剩余可用次数。它不会替代权限审核，只负责在权限通过后增加可用资源。',
        quantityLabel: '购买份数',
        actionTitle: '创建额度订单',
        actionDescription: '每份都会按当前配置增加固定额度，适合已经开通邮箱泛解析后继续扩容。',
        actionButtonLabel: '创建额度订单',
        totalSummary: (totalGrant: number) => `本次将增加 ${formatGrantAmount(totalGrant, product.grant_unit)} 的可用额度。`,
      };
    case 'payment_test_counter':
      return {
        headline: '验证支付链路是否工作正常',
        detail: '该项目不会给主业务权限加额度，只用于验证 LDC 支付、回调和订单状态刷新链路是否正常。',
        quantityLabel: '测试份数',
        actionTitle: '创建测试订单',
        actionDescription: '适合在正式兑换前先做一次小额验证，确认当前账号与支付环境都已经配置正常。',
        actionButtonLabel: '创建测试订单',
        totalSummary: (totalGrant: number) => `本次会记录 ${formatGrantAmount(totalGrant, product.grant_unit)} 的支付测试次数。`,
      };
    default:
      return {
        headline: product.display_name,
        detail: product.description,
        quantityLabel: '购买份数',
        actionTitle: '创建订单',
        actionDescription: '选择数量后直接创建订单，支付完成后由后端自动发放对应权益。',
        actionButtonLabel: '创建订单',
        totalSummary: (totalGrant: number) => `本次将发放 ${formatGrantAmount(totalGrant, product.grant_unit)}。`,
      };
  }
}

function normalizeIdentity(value: string): string {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '');
  return normalized || 'username';
}
