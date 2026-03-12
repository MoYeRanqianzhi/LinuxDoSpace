import { useEffect, useMemo, useState } from 'react';
import { CheckCircle2, CreditCard, FileText, LoaderCircle, Search, Settings2, XCircle } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { APIError, listAdminPaymentProducts, listApplications, listPermissionPolicies, updateAdminPaymentProduct, updateApplication, updatePermissionPolicy } from '../lib/api';
import { AdminSwitch } from '../components/AdminSwitch';
import { GlassCard } from '../components/GlassCard';
import type { AdminApplicationRecord, ApplicationStatus, PaymentProduct, PermissionPolicy } from '../types/admin';

interface ApplicationsPageProps {
  csrfToken: string;
}

interface PolicyDraft {
  enabled: boolean;
  auto_approve: boolean;
  min_trust_level: number;
  default_daily_limit?: number;
}

interface PaymentProductDraft {
  enabled: boolean;
  unit_price: string;
  grant_quantity: number;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}

function statusBadgeClass(status: ApplicationStatus): string {
  switch (status) {
    case 'approved':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300';
    case 'rejected':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300';
    case 'pending':
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300';
  }
}

function statusLabel(status: ApplicationStatus): string {
  switch (status) {
    case 'approved':
      return '已通过';
    case 'rejected':
      return '已拒绝';
    case 'pending':
    default:
      return '待审核';
  }
}

function typeLabel(type: AdminApplicationRecord['type']): string {
  switch (type) {
    case 'single':
      return '特定二级域名';
    case 'wildcard':
      return '泛解析';
    case 'email_catch_all':
      return '邮箱泛解析';
    case 'multiple':
    default:
      return '追加额度';
  }
}

function reviewSummary(record: AdminApplicationRecord): string {
  if (!record.reviewed_at || record.reviewed_by_user_id === undefined) {
    return '尚未产生审核记录';
  }
  return `管理员 #${record.reviewed_by_user_id} · ${formatDate(record.reviewed_at)}`;
}

function formatLDC(valueInCents: number): string {
  return (valueInCents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: valueInCents % 100 === 0 ? 0 : 2,
    maximumFractionDigits: 2,
  });
}

function readableGrantUnit(unit: string): string {
  switch (unit) {
    case 'day':
      return '天';
    case 'message':
      return '条';
    case 'run':
      return '次';
    default:
      return unit || '份';
  }
}

// ApplicationsPage renders both the administrator-facing application audit list
// and the policy controls that decide whether a permission can auto-approve.
export function ApplicationsPage({ csrfToken }: ApplicationsPageProps) {
  const [records, setRecords] = useState<AdminApplicationRecord[]>([]);
  const [policies, setPolicies] = useState<PermissionPolicy[]>([]);
  const [paymentProducts, setPaymentProducts] = useState<PaymentProduct[]>([]);
  const [policyDrafts, setPolicyDrafts] = useState<Record<string, PolicyDraft>>({});
  const [paymentProductDrafts, setPaymentProductDrafts] = useState<Record<string, PaymentProductDraft>>({});
  const [reviewDrafts, setReviewDrafts] = useState<Record<number, string>>({});
  const [statusDrafts, setStatusDrafts] = useState<Record<number, ApplicationStatus>>({});
  const [keyword, setKeyword] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [applicationsLoadError, setApplicationsLoadError] = useState('');
  const [policiesLoadError, setPoliciesLoadError] = useState('');
  const [paymentProductsLoadError, setPaymentProductsLoadError] = useState('');
  const [savingPolicyKeys, setSavingPolicyKeys] = useState<Record<string, boolean>>({});
  const [savingPaymentProductKeys, setSavingPaymentProductKeys] = useState<Record<string, boolean>>({});
  const [updatingApplicationIDs, setUpdatingApplicationIDs] = useState<Record<number, boolean>>({});

  // filteredRecords applies the shared search box to the application audit list.
  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [
        record.applicant_username,
        record.applicant_name,
        record.target,
        record.reason,
        record.review_note,
        record.status,
        typeLabel(record.type),
      ].some((field) => field.toLowerCase().includes(search)),
    );
  }, [keyword, records]);

  useEffect(() => {
    void loadData();
  }, []);

  // loadData refreshes both the policy editor and the application audit feed.
  async function loadData(): Promise<void> {
    try {
      setLoading(true);
      const [recordsResult, policiesResult, paymentProductsResult] = await Promise.allSettled([listApplications(), listPermissionPolicies(), listAdminPaymentProducts()]);

      if (recordsResult.status === 'fulfilled') {
        const nextRecords = recordsResult.value;
        setRecords(nextRecords);
        setReviewDrafts(Object.fromEntries(nextRecords.map((record) => [record.id, record.review_note])));
        setStatusDrafts(Object.fromEntries(nextRecords.map((record) => [record.id, record.status])));
        setApplicationsLoadError('');
      } else {
        setApplicationsLoadError(recordsResult.reason instanceof APIError ? recordsResult.reason.message : '应用申请列表加载失败。');
      }

      if (policiesResult.status === 'fulfilled') {
        const nextPolicies = policiesResult.value;
        setPolicies(nextPolicies);
        setPolicyDrafts(
          Object.fromEntries(
            nextPolicies.map((policy) => [
              policy.key,
              {
                enabled: policy.enabled,
                auto_approve: policy.auto_approve,
                min_trust_level: policy.min_trust_level,
                default_daily_limit: policy.default_daily_limit,
              },
            ]),
          ),
        );
        setPoliciesLoadError('');
      } else {
        setPoliciesLoadError(policiesResult.reason instanceof APIError ? policiesResult.reason.message : '权限策略加载失败。');
      }

      if (paymentProductsResult.status === 'fulfilled') {
        const nextProducts = paymentProductsResult.value;
        setPaymentProducts(nextProducts);
        setPaymentProductDrafts(
          Object.fromEntries(
            nextProducts.map((product) => [
              product.key,
              {
                enabled: product.enabled,
                unit_price: formatLDC(product.unit_price_cents),
                grant_quantity: product.grant_quantity,
              },
            ]),
          ),
        );
        setPaymentProductsLoadError('');
      } else {
        setPaymentProducts([]);
        setPaymentProductDrafts({});
        setPaymentProductsLoadError(paymentProductsResult.reason instanceof APIError ? paymentProductsResult.reason.message : 'LDC 商品配置加载失败。');
      }

      setError('');
      return;
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载申请记录失败。');
    } finally {
      setLoading(false);
    }
  }

  // saveApplication persists the selected status and review note for one record.
  async function saveApplication(record: AdminApplicationRecord): Promise<void> {
    try {
      setUpdatingApplicationIDs((current) => ({ ...current, [record.id]: true }));
      const updated = await updateApplication(
        record.id,
        {
          status: statusDrafts[record.id] ?? record.status,
          review_note: reviewDrafts[record.id] ?? '',
        },
        csrfToken,
      );
      setRecords((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      setReviewDrafts((current) => ({ ...current, [updated.id]: updated.review_note }));
      setStatusDrafts((current) => ({ ...current, [updated.id]: updated.status }));
      setError('');
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '更新申请状态失败。');
    } finally {
      setUpdatingApplicationIDs((current) => {
        const next = { ...current };
        delete next[record.id];
        return next;
      });
    }
  }

  // savePolicy persists one policy editor card back to the backend.
  async function savePolicy(policyKey: string): Promise<void> {
    const draft = policyDrafts[policyKey];
    if (!draft) {
      return;
    }

    try {
      setSavingPolicyKeys((current) => ({ ...current, [policyKey]: true }));
      const updated = await updatePermissionPolicy(
        policyKey,
        {
          enabled: draft.enabled,
          auto_approve: draft.auto_approve,
          min_trust_level: draft.min_trust_level,
          default_daily_limit: draft.default_daily_limit,
        },
        csrfToken,
      );
      setPolicies((current) => current.map((policy) => (policy.key === updated.key ? updated : policy)));
      setPolicyDrafts((current) => ({
        ...current,
        [updated.key]: {
            enabled: updated.enabled,
            auto_approve: updated.auto_approve,
            min_trust_level: updated.min_trust_level,
            default_daily_limit: updated.default_daily_limit,
          },
        }));
      setError('');
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '保存权限策略失败。');
    } finally {
      setSavingPolicyKeys((current) => {
        const next = { ...current };
        delete next[policyKey];
        return next;
      });
    }
  }

  async function savePaymentProduct(productKey: string): Promise<void> {
    const draft = paymentProductDrafts[productKey];
    if (!draft) {
      return;
    }

    try {
      setSavingPaymentProductKeys((current) => ({ ...current, [productKey]: true }));
      const updated = await updateAdminPaymentProduct(
        productKey,
        {
          enabled: draft.enabled,
          unit_price: draft.unit_price.trim(),
          grant_quantity: Math.max(1, Math.floor(draft.grant_quantity) || 1),
        },
        csrfToken,
      );
      setPaymentProducts((current) => current.map((item) => (item.key === updated.key ? updated : item)));
      setPaymentProductDrafts((current) => ({
        ...current,
        [updated.key]: {
          enabled: updated.enabled,
          unit_price: formatLDC(updated.unit_price_cents),
          grant_quantity: updated.grant_quantity,
        },
      }));
      setError('');
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '保存 LDC 商品配置失败。');
    } finally {
      setSavingPaymentProductKeys((current) => {
        const next = { ...current };
        delete next[productKey];
        return next;
      });
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-amber-100 p-3 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300">
            <FileText size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">申请与审核</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">统一处理权限申请、审核备注与自动通过策略。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索申请人、目标或备注"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      {error ? (
        <div className="mb-5 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
          {error}
        </div>
      ) : null}

      {policiesLoadError ? (
        <div className="mb-5 rounded-2xl border border-amber-300/50 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-950/30 dark:text-amber-100">
          {policiesLoadError}
        </div>
      ) : null}

      {paymentProductsLoadError ? (
        <div className="mb-5 rounded-2xl border border-amber-300/50 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-950/30 dark:text-amber-100">
          {paymentProductsLoadError}
        </div>
      ) : null}

      {applicationsLoadError ? (
        <div className="mb-5 rounded-2xl border border-amber-300/50 bg-amber-50/80 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-950/30 dark:text-amber-100">
          {applicationsLoadError}
        </div>
      ) : null}

      <div className="mb-8 grid gap-5 xl:grid-cols-2">
        {policies.map((policy) => {
          const draft = policyDrafts[policy.key] ?? {
            enabled: policy.enabled,
            auto_approve: policy.auto_approve,
            min_trust_level: policy.min_trust_level,
          };

          return (
            <GlassCard key={policy.key} className="p-6">
              <div className="mb-5 flex items-start justify-between gap-4">
                <div>
                  <div className="mb-2 inline-flex items-center gap-2 rounded-full bg-white/45 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-white/10 dark:text-slate-300">
                    <Settings2 size={14} />
                    策略配置
                  </div>
                  <h2 className="text-xl font-bold text-slate-900 dark:text-white">{policy.display_name}</h2>
                  <p className="mt-2 text-sm leading-7 text-slate-600 dark:text-slate-300">{policy.description}</p>
                </div>
                <span className={`rounded-full px-3 py-1 text-xs font-semibold ${draft.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                  {draft.enabled ? '已启用' : '已关闭'}
                </span>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <AdminSwitch
                  checked={draft.enabled}
                  onCheckedChange={(checked) =>
                    setPolicyDrafts((current) => ({
                      ...current,
                      [policy.key]: {
                        ...draft,
                        enabled: checked,
                      },
                    }))
                  }
                  label="允许申请"
                  description="关闭后，用户即使满足条件也无法提交申请。"
                  accent="amber"
                  className="border-white/20 bg-white/40 dark:border-white/10 dark:bg-black/20"
                />

                <AdminSwitch
                  checked={draft.auto_approve}
                  onCheckedChange={(checked) =>
                    setPolicyDrafts((current) => ({
                      ...current,
                      [policy.key]: {
                        ...draft,
                        auto_approve: checked,
                      },
                    }))
                  }
                  label="自动通过"
                  description="开启后，满足条件的用户申请会直接标记为 approved。"
                  accent="amber"
                  className="border-white/20 bg-white/40 dark:border-white/10 dark:bg-black/20"
                />
              </div>

              <div className="mt-4 rounded-2xl border border-white/20 bg-white/40 px-4 py-4 dark:border-white/10 dark:bg-black/20">
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">最低 Linux Do 等级</label>
                <input
                  type="number"
                  min={0}
                  max={4}
                  value={draft.min_trust_level}
                  onChange={(event) =>
                    setPolicyDrafts((current) => ({
                      ...current,
                      [policy.key]: {
                        ...draft,
                        min_trust_level: Number(event.target.value),
                      },
                    }))
                  }
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
                <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">Linux Do 当前信任等级范围为 0 到 4。</div>
              </div>

              <div className="mt-5 flex items-center justify-between gap-4">
                <div className="text-xs text-slate-500 dark:text-slate-400">最近更新：{formatDate(policy.updated_at)}</div>
                <button
                  onClick={() => void savePolicy(policy.key)}
                  disabled={Boolean(savingPolicyKeys[policy.key])}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-amber-500 to-orange-500 px-4 py-2 text-sm font-medium text-white shadow-lg transition hover:from-amber-600 hover:to-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {savingPolicyKeys[policy.key] ? <LoaderCircle size={16} className="animate-spin" /> : <CheckCircle2 size={16} />}
                  <span>{savingPolicyKeys[policy.key] ? '保存中...' : '保存策略'}</span>
                </button>
              </div>
            </GlassCard>
          );
        })}
      </div>

      <div className="mb-8 grid gap-5 xl:grid-cols-3">
        {paymentProducts.map((product) => {
          const draft = paymentProductDrafts[product.key] ?? {
            enabled: product.enabled,
            unit_price: formatLDC(product.unit_price_cents),
            grant_quantity: product.grant_quantity,
          };

          return (
            <GlassCard key={product.key} className="p-6">
              <div className="mb-5 flex items-start justify-between gap-4">
                <div>
                  <div className="mb-2 inline-flex items-center gap-2 rounded-full bg-white/45 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-white/10 dark:text-slate-300">
                    <CreditCard size={14} />
                    LDC 商品
                  </div>
                  <h2 className="text-xl font-bold text-slate-900 dark:text-white">{product.display_name}</h2>
                  <p className="mt-2 text-sm leading-7 text-slate-600 dark:text-slate-300">{product.description}</p>
                </div>
                <span className={`rounded-full px-3 py-1 text-xs font-semibold ${draft.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300' : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                  {draft.enabled ? '已启用' : '已关闭'}
                </span>
              </div>

              <AdminSwitch
                checked={draft.enabled}
                onCheckedChange={(checked) =>
                  setPaymentProductDrafts((current) => ({
                    ...current,
                    [product.key]: {
                      ...draft,
                      enabled: checked,
                    },
                  }))
                }
                label="允许前台兑换"
                description="关闭后，前台将不再展示该 LDC 商品。"
                accent="amber"
                className="border-white/20 bg-white/40 dark:border-white/10 dark:bg-black/20"
              />

              <div className="mt-4 grid gap-4">
                <div className="rounded-2xl border border-white/20 bg-white/40 px-4 py-4 dark:border-white/10 dark:bg-black/20">
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">单价（LDC）</label>
                  <input
                    type="number"
                    min={0.01}
                    step={0.01}
                    value={draft.unit_price}
                    onChange={(event) =>
                      setPaymentProductDrafts((current) => ({
                        ...current,
                        [product.key]: {
                          ...draft,
                          unit_price: event.target.value,
                        },
                      }))
                    }
                    className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                  />
                </div>

                <div className="rounded-2xl border border-white/20 bg-white/40 px-4 py-4 dark:border-white/10 dark:bg-black/20">
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">单份发放数量</label>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={draft.grant_quantity}
                    onChange={(event) =>
                      setPaymentProductDrafts((current) => ({
                        ...current,
                        [product.key]: {
                          ...draft,
                          grant_quantity: Math.max(1, Number(event.target.value) || 1),
                        },
                      }))
                    }
                    className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                  />
                  <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">当前发放单位：{readableGrantUnit(product.grant_unit)}</div>
                </div>
              </div>

              <div className="mt-5 flex items-center justify-between gap-4">
                <div className="text-xs text-slate-500 dark:text-slate-400">最近更新：{formatDate(product.updated_at)}</div>
                <button
                  onClick={() => void savePaymentProduct(product.key)}
                  disabled={Boolean(savingPaymentProductKeys[product.key])}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-4 py-2 text-sm font-medium text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {savingPaymentProductKeys[product.key] ? <LoaderCircle size={16} className="animate-spin" /> : <CheckCircle2 size={16} />}
                  <span>{savingPaymentProductKeys[product.key] ? '保存中...' : '保存商品'}</span>
                </button>
              </div>
            </GlassCard>
          );
        })}
      </div>

      <div className="space-y-4">
        {loading ? (
          <GlassCard className="p-8 text-center text-sm text-slate-500 dark:text-slate-300">正在加载申请记录...</GlassCard>
        ) : null}

        {!loading ? (
          <AnimatePresence>
            {filteredRecords.map((record) => {
              const statusDraft = statusDrafts[record.id] ?? record.status;
              const reviewDraft = reviewDrafts[record.id] ?? '';
              const dirty = statusDraft !== record.status || reviewDraft !== record.review_note;

              return (
                <motion.div key={record.id} layout initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, x: -32 }}>
                  <GlassCard className="p-6">
                    <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
                      <div className="space-y-4">
                        <div className="flex flex-wrap items-center gap-3">
                          <span className="text-lg font-bold text-slate-900 dark:text-white">{record.applicant_username}</span>
                          <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                            {formatDate(record.created_at)}
                          </span>
                          <span className={`rounded-full px-3 py-1 text-xs font-semibold ${statusBadgeClass(record.status)}`}>
                            {statusLabel(record.status)}
                          </span>
                        </div>

                        <div className="grid gap-4 md:grid-cols-2">
                          <div>
                            <div className="mb-1 text-xs uppercase tracking-[0.24em] text-slate-400">申请类型</div>
                            <div className="font-semibold text-slate-900 dark:text-white">{typeLabel(record.type)}</div>
                          </div>
                          <div>
                            <div className="mb-1 text-xs uppercase tracking-[0.24em] text-slate-400">目标对象</div>
                            <div className="font-mono text-amber-600 dark:text-amber-300">{record.target}</div>
                          </div>
                          <div>
                            <div className="mb-1 text-xs uppercase tracking-[0.24em] text-slate-400">申请人昵称</div>
                            <div className="font-semibold text-slate-900 dark:text-white">{record.applicant_name}</div>
                          </div>
                          <div>
                            <div className="mb-1 text-xs uppercase tracking-[0.24em] text-slate-400">最近审核</div>
                            <div className="text-sm text-slate-600 dark:text-slate-300">{reviewSummary(record)}</div>
                          </div>
                        </div>

                        <div>
                          <div className="mb-2 text-xs uppercase tracking-[0.24em] text-slate-400">申请理由</div>
                          <div className="rounded-2xl border border-white/20 bg-white/35 px-4 py-4 text-sm leading-6 text-slate-700 dark:border-white/10 dark:bg-black/25 dark:text-slate-200">
                            {record.reason}
                          </div>
                        </div>

                        <div>
                          <div className="mb-2 text-xs uppercase tracking-[0.24em] text-slate-400">审核状态</div>
                          <div className="flex flex-wrap gap-2">
                            {(['pending', 'approved', 'rejected'] as ApplicationStatus[]).map((status) => {
                              const selected = statusDraft === status;
                              const baseClass = status === 'approved'
                                ? 'border-emerald-200 text-emerald-700 dark:border-emerald-900/60 dark:text-emerald-300'
                                : status === 'rejected'
                                  ? 'border-red-200 text-red-700 dark:border-red-900/60 dark:text-red-300'
                                  : 'border-amber-200 text-amber-700 dark:border-amber-900/60 dark:text-amber-300';
                              const selectedClass = status === 'approved'
                                ? 'bg-emerald-500 text-white border-emerald-500'
                                : status === 'rejected'
                                  ? 'bg-red-500 text-white border-red-500'
                                  : 'bg-amber-500 text-white border-amber-500';

                              return (
                                <button
                                  key={status}
                                  onClick={() => setStatusDrafts((current) => ({ ...current, [record.id]: status }))}
                                  disabled={Boolean(updatingApplicationIDs[record.id])}
                                  className={`rounded-2xl border px-4 py-2 text-sm font-medium transition ${selected ? selectedClass : `bg-white/55 dark:bg-black/20 ${baseClass}`}`}
                                >
                                  {statusLabel(status)}
                                </button>
                              );
                            })}
                          </div>
                        </div>

                        <div>
                          <div className="mb-2 text-xs uppercase tracking-[0.24em] text-slate-400">审核备注</div>
                          <textarea
                            value={reviewDraft}
                            onChange={(event) =>
                              setReviewDrafts((current) => ({
                                ...current,
                                [record.id]: event.target.value,
                              }))
                            }
                            rows={3}
                            className="w-full rounded-2xl border border-white/20 bg-white/55 px-4 py-3 text-sm leading-6 text-slate-700 outline-none transition focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-white/10 dark:bg-black/25 dark:text-slate-200"
                          />
                        </div>
                      </div>

                      <div className="flex min-w-56 flex-col gap-3 lg:max-w-56">
                        <div className="rounded-2xl border border-white/20 bg-white/35 px-4 py-4 text-sm text-slate-600 dark:border-white/10 dark:bg-black/25 dark:text-slate-300">
                          <div className="font-semibold text-slate-900 dark:text-white">当前审核草稿</div>
                          <div className="mt-2">状态：{statusLabel(statusDraft)}</div>
                          <div className="mt-1">备注长度：{reviewDraft.trim().length} 字</div>
                          <div className="mt-1">变更：{dirty ? '未保存' : '已同步'}</div>
                        </div>

                        <button
                          onClick={() => void saveApplication(record)}
                          disabled={Boolean(updatingApplicationIDs[record.id]) || !dirty}
                          className="flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-amber-500 to-orange-500 px-5 py-3 text-sm font-medium text-white shadow-lg transition hover:from-amber-600 hover:to-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
                        >
                          {updatingApplicationIDs[record.id] ? <LoaderCircle size={18} className="animate-spin" /> : <CheckCircle2 size={18} />}
                          <span>{updatingApplicationIDs[record.id] ? '保存中...' : '保存审核'}</span>
                        </button>

                        <button
                          onClick={() => {
                            setStatusDrafts((current) => ({ ...current, [record.id]: record.status }));
                            setReviewDrafts((current) => ({ ...current, [record.id]: record.review_note }));
                          }}
                          disabled={Boolean(updatingApplicationIDs[record.id]) || !dirty}
                          className="flex items-center justify-center gap-2 rounded-2xl bg-slate-100 px-5 py-3 text-sm font-medium text-slate-700 transition hover:bg-slate-200 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700"
                        >
                          <XCircle size={18} />
                          <span>撤销草稿</span>
                        </button>
                      </div>
                    </div>
                  </GlassCard>
                </motion.div>
              );
            })}
          </AnimatePresence>
        ) : null}
      </div>
    </div>
  );
}
