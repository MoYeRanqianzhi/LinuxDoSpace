import { useEffect, useMemo, useState } from 'react';
import { Cloud, Edit2, Plus, Search, ShieldCheck, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import {
  APIError,
  createAdminRecord,
  deleteAdminRecord,
  listAdminAllocations,
  listAdminRecords,
  listManagedDomains,
  updateAdminRecord,
  upsertManagedDomain,
} from '../lib/api';
import { GlassCard } from '../components/GlassCard';
import type {
  AdminAllocationRecord,
  AdminDomainRecord,
  ManagedDomain,
  UpsertAdminDomainRecordInput,
  UpsertManagedDomainInput,
} from '../types/admin';

interface DomainsPageProps {
  csrfToken: string;
  managedDomains: ManagedDomain[];
  onManagedDomainsChange: (domains: ManagedDomain[]) => void;
}

const blankRecordDraft: UpsertAdminDomainRecordInput = {
  type: 'A',
  name: '@',
  content: '',
  ttl: 1,
  proxied: true,
  comment: '',
};

const blankManagedDomainDraft: UpsertManagedDomainInput = {
  root_domain: '',
  cloudflare_zone_id: '',
  default_quota: 1,
  auto_provision: true,
  is_default: false,
  enabled: true,
};

export function DomainsPage({ csrfToken, managedDomains, onManagedDomainsChange }: DomainsPageProps) {
  const [records, setRecords] = useState<AdminDomainRecord[]>([]);
  const [allocations, setAllocations] = useState<AdminAllocationRecord[]>([]);
  const [keyword, setKeyword] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editingRecord, setEditingRecord] = useState<AdminDomainRecord | null>(null);
  const [creatingAllocationID, setCreatingAllocationID] = useState<number>(0);
  const [creatingRecordDraft, setCreatingRecordDraft] = useState<UpsertAdminDomainRecordInput>(blankRecordDraft);
  const [managedDomainDraft, setManagedDomainDraft] = useState<UpsertManagedDomainInput | null>(null);
  const [saving, setSaving] = useState(false);

  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [record.owner_username, record.name, record.type, record.content, record.namespace_fqdn].some((field) =>
        field.toLowerCase().includes(search),
      ),
    );
  }, [keyword, records]);

  async function loadData() {
    try {
      setLoading(true);
      const [nextRecords, nextAllocations] = await Promise.all([listAdminRecords(), listAdminAllocations()]);
      setRecords(nextRecords);
      setAllocations(nextAllocations);
      setError('');
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载域名数据失败。');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadData();
  }, []);

  async function refreshManagedDomains() {
    try {
      const nextDomains = await listManagedDomains();
      onManagedDomainsChange(nextDomains);
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '刷新根域名配置失败。');
    }
  }

  async function saveManagedDomain() {
    if (!managedDomainDraft) {
      return;
    }
    try {
      setSaving(true);
      await upsertManagedDomain(managedDomainDraft, csrfToken);
      await refreshManagedDomains();
      setManagedDomainDraft(null);
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '保存根域名配置失败。');
    } finally {
      setSaving(false);
    }
  }

  async function saveEditedRecord() {
    if (!editingRecord) {
      return;
    }
    try {
      setSaving(true);
      const updated = await updateAdminRecord(
        editingRecord.allocation_id,
        editingRecord.id,
        {
          type: editingRecord.type,
          name: editingRecord.relative_name,
          content: editingRecord.content,
          ttl: editingRecord.ttl,
          proxied: editingRecord.proxied,
          comment: editingRecord.comment,
          priority: editingRecord.priority,
        },
        csrfToken,
      );
      setRecords((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      setEditingRecord(null);
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '保存解析记录失败。');
    } finally {
      setSaving(false);
    }
  }

  async function submitCreateRecord() {
    try {
      setSaving(true);
      const created = await createAdminRecord(creatingAllocationID, creatingRecordDraft, csrfToken);
      setRecords((current) => [created, ...current]);
      setCreatingAllocationID(0);
      setCreatingRecordDraft(blankRecordDraft);
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '创建解析记录失败。');
    } finally {
      setSaving(false);
    }
  }

  async function removeRecord(record: AdminDomainRecord) {
    try {
      await deleteAdminRecord(record.allocation_id, record.id, csrfToken);
      setRecords((current) => current.filter((item) => item.id !== record.id));
    } catch (deleteError) {
      setError(deleteError instanceof APIError ? deleteError.message : '删除解析记录失败。');
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-blue-100 p-3 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300">
            <Cloud size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">域名管理</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">同时管理根域名配置、Cloudflare 解析记录与命名空间归属。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索用户、主机名或记录内容"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      {error ? (
        <div className="mb-5 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
          {error}
        </div>
      ) : null}

      <div className="mb-6 grid gap-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)]">
        <GlassCard>
          <div className="mb-4 flex items-center justify-between gap-3">
            <div>
              <h2 className="text-xl font-bold text-slate-900 dark:text-white">根域名配置</h2>
              <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">默认配额、Cloudflare Zone、自动分配和启用状态都在这里控制。</p>
            </div>
            <button
              onClick={() => setManagedDomainDraft({ ...blankManagedDomainDraft })}
              className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-blue-500 to-indigo-500 px-4 py-2 text-sm font-medium text-white shadow-lg"
            >
              <Plus size={16} />
              <span>新增根域名</span>
            </button>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            {managedDomains.map((domain) => (
              <div key={domain.id} className="rounded-2xl border border-white/20 bg-white/35 p-4 dark:border-white/10 dark:bg-black/25">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="font-semibold text-slate-900 dark:text-white">{domain.root_domain}</div>
                    <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">Zone: {domain.cloudflare_zone_id || '自动解析'}</div>
                  </div>
                  <button
                    onClick={() =>
                      setManagedDomainDraft({
                        root_domain: domain.root_domain,
                        cloudflare_zone_id: domain.cloudflare_zone_id,
                        default_quota: domain.default_quota,
                        auto_provision: domain.auto_provision,
                        is_default: domain.is_default,
                        enabled: domain.enabled,
                      })
                    }
                    className="rounded-xl p-2 text-blue-500 transition hover:bg-blue-100 dark:hover:bg-blue-900/25"
                  >
                    <Edit2 size={16} />
                  </button>
                </div>
                <div className="mt-4 grid gap-2 text-sm text-slate-600 dark:text-slate-300">
                  <div>默认配额：{domain.default_quota}</div>
                  <div>自动分配：{domain.auto_provision ? '开启' : '关闭'}</div>
                  <div>默认域名：{domain.is_default ? '是' : '否'}</div>
                  <div>状态：{domain.enabled ? '启用中' : '已停用'}</div>
                </div>
              </div>
            ))}
          </div>
        </GlassCard>

        <GlassCard>
          <div className="mb-4 flex items-center gap-2 text-xl font-bold text-slate-900 dark:text-white">
            <Plus size={18} className="text-indigo-500" />
            新建解析记录
          </div>
          <div className="space-y-4">
            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">所属命名空间</label>
              <select
                value={creatingAllocationID}
                onChange={(event) => setCreatingAllocationID(Number(event.target.value))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              >
                <option value={0}>请选择命名空间</option>
                {allocations.map((allocation) => (
                  <option key={allocation.id} value={allocation.id}>
                    {allocation.fqdn} · {allocation.owner_username}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">记录名</label>
                <input
                  value={creatingRecordDraft.name}
                  onChange={(event) => setCreatingRecordDraft((current) => ({ ...current, name: event.target.value }))}
                  className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">类型</label>
                <select
                  value={creatingRecordDraft.type}
                  onChange={(event) =>
                    setCreatingRecordDraft((current) => ({
                      ...current,
                      type: event.target.value as UpsertAdminDomainRecordInput['type'],
                      proxied: event.target.value === 'TXT' || event.target.value === 'MX' ? false : current.proxied,
                    }))
                  }
                  className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                >
                  <option value="A">A</option>
                  <option value="AAAA">AAAA</option>
                  <option value="CNAME">CNAME</option>
                  <option value="TXT">TXT</option>
                  <option value="MX">MX</option>
                </select>
              </div>
            </div>
            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">内容</label>
              <input
                value={creatingRecordDraft.content}
                onChange={(event) => setCreatingRecordDraft((current) => ({ ...current, content: event.target.value }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">TTL</label>
                <input
                  type="number"
                  min={1}
                  value={creatingRecordDraft.ttl}
                  onChange={(event) => setCreatingRecordDraft((current) => ({ ...current, ttl: Math.max(1, Number(event.target.value) || 1) }))}
                  className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              {creatingRecordDraft.type === 'MX' ? (
                <div>
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">优先级</label>
                  <input
                    type="number"
                    min={1}
                    value={creatingRecordDraft.priority ?? 10}
                    onChange={(event) =>
                      setCreatingRecordDraft((current) => ({
                        ...current,
                        priority: Math.max(1, Number(event.target.value) || 10),
                      }))
                    }
                    className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                  />
                </div>
              ) : null}
            </div>
            <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
              <input
                type="checkbox"
                checked={creatingRecordDraft.proxied}
                disabled={creatingRecordDraft.type === 'TXT' || creatingRecordDraft.type === 'MX'}
                onChange={(event) => setCreatingRecordDraft((current) => ({ ...current, proxied: event.target.checked }))}
              />
              通过 Cloudflare 代理
            </label>
            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">备注</label>
              <input
                value={creatingRecordDraft.comment}
                onChange={(event) => setCreatingRecordDraft((current) => ({ ...current, comment: event.target.value }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              />
            </div>
            <button
              onClick={() => void submitCreateRecord()}
              disabled={saving || creatingAllocationID <= 0 || !creatingRecordDraft.content.trim()}
              className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-indigo-500 to-violet-500 px-4 py-3 font-medium text-white shadow-lg transition hover:from-indigo-600 hover:to-violet-600 disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Plus size={18} />
              <span>{saving ? '提交中...' : '创建解析'}</span>
            </button>
          </div>
        </GlassCard>
      </div>

      <GlassCard className="overflow-hidden p-0">
        <div className="custom-scrollbar overflow-x-auto">
          <table className="min-w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-white/5">
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">所属用户</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">主机名</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">类型</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">内容</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">代理</th>
                <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                    正在加载解析记录...
                  </td>
                </tr>
              ) : null}
              {!loading ? (
                <AnimatePresence>
                  {filteredRecords.map((record) => (
                    <motion.tr
                      key={record.id}
                      layout
                      initial={{ opacity: 0, y: 10 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, x: -30 }}
                      className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5"
                    >
                      <td className="px-5 py-4 font-medium text-slate-900 dark:text-white">{record.owner_username}</td>
                      <td className="px-5 py-4">
                        <div className="font-mono text-blue-600 dark:text-blue-300">{record.name}</div>
                        <div className="mt-1 text-xs text-slate-400">命名空间：{record.namespace_fqdn}</div>
                      </td>
                      <td className="px-5 py-4">
                        <span className="rounded-lg bg-slate-100 px-2 py-1 text-xs font-semibold dark:bg-slate-800">{record.type}</span>
                      </td>
                      <td className="px-5 py-4 font-mono text-slate-600 dark:text-slate-300">{record.content}</td>
                      <td className="px-5 py-4">
                        <span
                          className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-semibold ${
                            record.proxied
                              ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                              : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                          }`}
                        >
                          <ShieldCheck size={12} />
                          {record.proxied ? '已开启' : '未开启'}
                        </span>
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex justify-end gap-2">
                          <button
                            onClick={() => setEditingRecord({ ...record })}
                            className="rounded-xl p-2 text-blue-500 transition hover:bg-blue-100 dark:hover:bg-blue-900/25"
                            aria-label={`编辑 ${record.name}`}
                          >
                            <Edit2 size={16} />
                          </button>
                          <button
                            onClick={() => void removeRecord(record)}
                            className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white"
                            aria-label={`删除 ${record.name}`}
                          >
                            <Trash2 size={16} />
                          </button>
                        </div>
                      </td>
                    </motion.tr>
                  ))}
                </AnimatePresence>
              ) : null}
            </tbody>
          </table>
        </div>
      </GlassCard>

      {editingRecord ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
          <button className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={() => setEditingRecord(null)} aria-label="关闭编辑弹窗" />
          <GlassCard className="relative z-10 w-full max-w-lg border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">编辑解析记录</h2>
            <div className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">主机名</label>
                <input value={editingRecord.name} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">记录内容</label>
                <input
                  value={editingRecord.content}
                  onChange={(event) => setEditingRecord({ ...editingRecord, content: event.target.value })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">TTL</label>
                  <input
                    type="number"
                    min={1}
                    value={editingRecord.ttl}
                    onChange={(event) => setEditingRecord({ ...editingRecord, ttl: Math.max(1, Number(event.target.value) || 1) })}
                    className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                  />
                </div>
                {editingRecord.type === 'MX' ? (
                  <div>
                    <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">优先级</label>
                    <input
                      type="number"
                      min={1}
                      value={editingRecord.priority ?? 10}
                      onChange={(event) => setEditingRecord({ ...editingRecord, priority: Math.max(1, Number(event.target.value) || 10) })}
                      className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                    />
                  </div>
                ) : null}
              </div>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input
                  type="checkbox"
                  checked={editingRecord.proxied}
                  disabled={editingRecord.type === 'TXT' || editingRecord.type === 'MX'}
                  onChange={(event) => setEditingRecord({ ...editingRecord, proxied: event.target.checked })}
                />
                通过 Cloudflare 代理访问
              </label>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">备注</label>
                <input
                  value={editingRecord.comment}
                  onChange={(event) => setEditingRecord({ ...editingRecord, comment: event.target.value })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => setEditingRecord(null)} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">取消</button>
              <button onClick={() => void saveEditedRecord()} disabled={saving} className="flex-1 rounded-2xl bg-gradient-to-r from-blue-500 to-indigo-500 px-4 py-3 font-medium text-white disabled:cursor-not-allowed disabled:opacity-60">{saving ? '保存中...' : '保存'}</button>
            </div>
          </GlassCard>
        </div>
      ) : null}

      {managedDomainDraft ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
          <button className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={() => setManagedDomainDraft(null)} aria-label="关闭根域名编辑弹窗" />
          <GlassCard className="relative z-10 w-full max-w-xl border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">根域名配置</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">根域名</label>
                <input
                  value={managedDomainDraft.root_domain}
                  onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, root_domain: event.target.value })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <div className="sm:col-span-2">
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">Cloudflare Zone ID</label>
                <input
                  value={managedDomainDraft.cloudflare_zone_id}
                  onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, cloudflare_zone_id: event.target.value })}
                  placeholder="留空时由后端自动解析"
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">默认配额</label>
                <input
                  type="number"
                  min={1}
                  value={managedDomainDraft.default_quota}
                  onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, default_quota: Math.max(1, Number(event.target.value) || 1) })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input type="checkbox" checked={managedDomainDraft.auto_provision} onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, auto_provision: event.target.checked })} />
                登录后自动分配同名子域名
              </label>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input type="checkbox" checked={managedDomainDraft.is_default} onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, is_default: event.target.checked })} />
                设为默认根域名
              </label>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input type="checkbox" checked={managedDomainDraft.enabled} onChange={(event) => setManagedDomainDraft({ ...managedDomainDraft, enabled: event.target.checked })} />
                允许继续分发
              </label>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => setManagedDomainDraft(null)} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">取消</button>
              <button onClick={() => void saveManagedDomain()} disabled={saving} className="flex-1 rounded-2xl bg-gradient-to-r from-blue-500 to-indigo-500 px-4 py-3 font-medium text-white disabled:cursor-not-allowed disabled:opacity-60">{saving ? '保存中...' : '保存'}</button>
            </div>
          </GlassCard>
        </div>
      ) : null}
    </div>
  );
}
