import { useEffect, useMemo, useState } from 'react';
import { ArrowRight, Edit2, Mail, Plus, Search, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { APIError, createEmailRoute, deleteEmailRoute, listAdminUsers, listEmailRoutes, updateEmailRoute } from '../lib/api';
import { AdminSelect } from '../components/AdminSelect';
import { GlassCard } from '../components/GlassCard';
import type { AdminEmailRecord, AdminUserRecord, ManagedDomain, UpdateEmailRouteInput, UpsertEmailRouteInput } from '../types/admin';

interface EmailsPageProps {
  csrfToken: string;
  managedDomains: ManagedDomain[];
}

const blankRouteDraft: UpsertEmailRouteInput = {
  owner_user_id: 0,
  root_domain: '',
  prefix: '',
  target_email: '',
  enabled: true,
};

// validateEmailRouteInput performs the small client-side checks that keep the
// admin modal from hiding obvious mistakes behind a generic API failure.
function validateEmailRouteInput(prefix: string, targetEmail: string): string {
  if (!prefix.trim()) {
    return '邮箱前缀不能为空。';
  }
  if (!targetEmail.trim()) {
    return '目标邮箱不能为空。';
  }
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(targetEmail.trim())) {
    return '目标邮箱格式不正确。';
  }
  return '';
}

export function EmailsPage({ csrfToken, managedDomains }: EmailsPageProps) {
  const [records, setRecords] = useState<AdminEmailRecord[]>([]);
  const [users, setUsers] = useState<AdminUserRecord[]>([]);
  const [keyword, setKeyword] = useState('');
  const [draft, setDraft] = useState<UpsertEmailRouteInput>({
    ...blankRouteDraft,
    root_domain: managedDomains[0]?.root_domain ?? '',
  });
  const [editingRecord, setEditingRecord] = useState<AdminEmailRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [modalError, setModalError] = useState('');

  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [record.owner_username, record.prefix, record.target_email, record.root_domain].some((field) =>
        field.toLowerCase().includes(search),
      ),
    );
  }, [keyword, records]);

  useEffect(() => {
    setDraft((current) => ({
      ...current,
      root_domain: current.root_domain || managedDomains[0]?.root_domain || '',
    }));
  }, [managedDomains]);

  async function loadData() {
    try {
      setLoading(true);
      const [routesResult, usersResult] = await Promise.allSettled([listEmailRoutes(), listAdminUsers()]);
      const loadErrors: string[] = [];

      if (routesResult.status === 'fulfilled') {
        setRecords(routesResult.value);
      } else {
        setRecords([]);
        loadErrors.push(routesResult.reason instanceof APIError ? routesResult.reason.message : '加载邮箱路由失败。');
      }

      if (usersResult.status === 'fulfilled') {
        setUsers(usersResult.value);
      } else {
        setUsers([]);
        loadErrors.push(usersResult.reason instanceof APIError ? usersResult.reason.message : '加载用户列表失败。');
      }

      setError(loadErrors.join(' '));
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载邮箱路由失败。');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadData();
  }, []);

  async function submitCreate() {
    const validationError = validateEmailRouteInput(draft.prefix, draft.target_email);
    if (draft.owner_user_id <= 0) {
      setError('请选择所属用户。');
      return;
    }
    if (!draft.root_domain.trim()) {
      setError('请选择根域名。');
      return;
    }
    if (validationError) {
      setError(validationError);
      return;
    }

    try {
      setSaving(true);
      setError('');
      const created = await createEmailRoute(draft, csrfToken);
      setRecords((current) => [created, ...current]);
      setDraft({ ...blankRouteDraft, root_domain: managedDomains[0]?.root_domain ?? '' });
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '创建邮箱路由失败。');
    } finally {
      setSaving(false);
    }
  }

  async function saveEditingRecord() {
    if (!editingRecord) {
      return;
    }

    const validationError = validateEmailRouteInput(editingRecord.prefix, editingRecord.target_email);
    if (validationError) {
      setModalError(validationError);
      return;
    }

    try {
      setSaving(true);
      setError('');
      setModalError('');
      const updateInput: UpdateEmailRouteInput = {
        target_email: editingRecord.target_email,
        enabled: editingRecord.enabled,
      };
      const updated = await updateEmailRoute(
        editingRecord.id,
        updateInput,
        csrfToken,
      );
      setRecords((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      setEditingRecord(null);
    } catch (saveError) {
      const message = saveError instanceof APIError ? saveError.message : '保存邮箱路由失败。';
      setError(message);
      setModalError(message);
    } finally {
      setSaving(false);
    }
  }

  async function removeRecord(id: number) {
    try {
      await deleteEmailRoute(id, csrfToken);
      setRecords((current) => current.filter((record) => record.id !== id));
    } catch (deleteError) {
      setError(deleteError instanceof APIError ? deleteError.message : '删除邮箱路由失败。');
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-fuchsia-100 p-3 text-fuchsia-600 dark:bg-fuchsia-900/30 dark:text-fuchsia-300">
            <Mail size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">邮箱管理</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">维护专属邮箱别名与收件目标地址的转发映射。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索用户、前缀或目标邮箱"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      {error ? (
        <div className="mb-5 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
          {error}
        </div>
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]">
        <GlassCard>
          <div className="space-y-4">
            <h2 className="flex items-center gap-2 text-xl font-bold text-slate-900 dark:text-white">
              <Plus size={18} className="text-fuchsia-500" />
              新建邮箱转发
            </h2>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">所属用户</label>
              <AdminSelect
                value={draft.owner_user_id}
                onChange={(event) => setDraft((current) => ({ ...current, owner_user_id: Number(event.target.value) }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              >
                <option value={0}>请选择用户</option>
                {users.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.username}
                  </option>
                ))}
              </AdminSelect>
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">根域名</label>
              <AdminSelect
                value={draft.root_domain}
                onChange={(event) => setDraft((current) => ({ ...current, root_domain: event.target.value }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              >
                <option value="">请选择根域名</option>
                {managedDomains.map((domain) => (
                  <option key={domain.id} value={domain.root_domain}>
                    {domain.root_domain}
                  </option>
                ))}
              </AdminSelect>
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">邮箱前缀</label>
              <input
                value={draft.prefix}
                onChange={(event) => setDraft((current) => ({ ...current, prefix: event.target.value }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              />
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">目标邮箱</label>
              <input
                type="email"
                value={draft.target_email}
                onChange={(event) => setDraft((current) => ({ ...current, target_email: event.target.value }))}
                className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
              />
            </div>

            <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
              <input type="checkbox" checked={draft.enabled} onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))} />
              创建后立即启用
            </label>

            <button
              onClick={() => void submitCreate()}
              disabled={saving || draft.owner_user_id <= 0 || !draft.root_domain || !draft.prefix.trim() || !draft.target_email.trim()}
              className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-fuchsia-500 to-pink-500 px-4 py-3 font-medium text-white shadow-lg transition hover:from-fuchsia-600 hover:to-pink-600 disabled:cursor-not-allowed disabled:opacity-60"
            >
              <Plus size={18} />
              <span>{saving ? '创建中...' : '创建转发'}</span>
            </button>
          </div>
        </GlassCard>

        <GlassCard className="overflow-hidden p-0">
          <div className="custom-scrollbar overflow-x-auto">
            <table className="min-w-full border-collapse text-left">
              <thead>
                <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-white/5">
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">所属用户</th>
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">专属邮箱</th>
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">目标转发</th>
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">状态</th>
                  <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr>
                    <td colSpan={5} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                      正在加载邮箱路由...
                    </td>
                  </tr>
                ) : null}
                {!loading ? (
                  <AnimatePresence>
                    {filteredRecords.map((record) => (
                      <motion.tr key={record.id} layout initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, x: -30 }} className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                        <td className="px-5 py-4 font-medium text-slate-900 dark:text-white">{record.owner_username}</td>
                        <td className="px-5 py-4 font-medium text-fuchsia-600 dark:text-fuchsia-300">{record.prefix}@{record.root_domain}</td>
                        <td className="px-5 py-4 text-slate-600 dark:text-slate-300">
                          <span className="inline-flex items-center gap-2"><ArrowRight size={15} /> {record.target_email}</span>
                        </td>
                        <td className="px-5 py-4">
                          <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-semibold ${record.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                            {record.enabled ? '启用中' : '已停用'}
                          </span>
                        </td>
                        <td className="px-5 py-4">
                          <div className="flex justify-end gap-2">
                            <button onClick={() => { setEditingRecord({ ...record }); setModalError(''); }} className="rounded-xl p-2 text-fuchsia-500 transition hover:bg-fuchsia-100 dark:hover:bg-fuchsia-900/25" aria-label={`编辑 ${record.prefix}`}><Edit2 size={16} /></button>
                            <button onClick={() => void removeRecord(record.id)} className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white" aria-label={`删除 ${record.prefix}`}><Trash2 size={16} /></button>
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
      </div>

      {editingRecord ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
          <button className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={() => { setEditingRecord(null); setModalError(''); }} aria-label="关闭编辑弹窗" />
          <GlassCard className="relative z-10 w-full max-w-lg border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">编辑邮箱转发</h2>
            {modalError ? (
              <div className="mb-4 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
                {modalError}
              </div>
            ) : null}
            <div className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">邮箱地址</label>
                <div className="rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400">
                  {editingRecord.prefix}@{editingRecord.root_domain}
                </div>
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">目标邮箱</label>
                <input type="email" value={editingRecord.target_email} onChange={(event) => setEditingRecord({ ...editingRecord, target_email: event.target.value })} className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-fuchsia-400 focus:ring-2 focus:ring-fuchsia-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
              </div>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input type="checkbox" checked={editingRecord.enabled} onChange={(event) => setEditingRecord({ ...editingRecord, enabled: event.target.checked })} />
                保持启用
              </label>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => { setEditingRecord(null); setModalError(''); }} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">取消</button>
              <button onClick={() => void saveEditingRecord()} disabled={saving} className="flex-1 rounded-2xl bg-gradient-to-r from-fuchsia-500 to-pink-500 px-4 py-3 font-medium text-white disabled:cursor-not-allowed disabled:opacity-60">{saving ? '保存中...' : '保存'}</button>
            </div>
          </GlassCard>
        </div>
      ) : null}
    </div>
  );
}
