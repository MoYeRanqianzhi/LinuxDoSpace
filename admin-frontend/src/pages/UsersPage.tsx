import { useEffect, useMemo, useState } from 'react';
import { Ban, CheckCircle2, Edit2, Search, Shield, Users } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { APIError, getAdminUserDetail, listAdminUsers, setUserQuota, updateAdminUser } from '../lib/api';
import { GlassCard } from '../components/GlassCard';
import type { AdminUserDetail, AdminUserRecord, ManagedDomain } from '../types/admin';

interface UsersPageProps {
  csrfToken: string;
  managedDomains: ManagedDomain[];
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}

export function UsersPage({ csrfToken, managedDomains }: UsersPageProps) {
  const [records, setRecords] = useState<AdminUserRecord[]>([]);
  const [keyword, setKeyword] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editingDetail, setEditingDetail] = useState<AdminUserDetail | null>(null);
  const [editingLoading, setEditingLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [draftBanned, setDraftBanned] = useState(false);
  const [draftBanNote, setDraftBanNote] = useState('');
  const [draftQuotas, setDraftQuotas] = useState<Record<string, number>>({});

  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [
        record.username,
        record.display_name,
        record.is_banned ? 'banned' : 'active',
        String(record.trust_level),
      ].some((field) => field.toLowerCase().includes(search)),
    );
  }, [keyword, records]);

  async function loadUsers() {
    try {
      setLoading(true);
      const data = await listAdminUsers();
      setRecords(data);
      setError('');
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载用户列表失败。');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadUsers();
  }, []);

  async function openEditor(record: AdminUserRecord) {
    try {
      setEditingLoading(true);
      const detail = await getAdminUserDetail(record.id);
      setEditingDetail(detail);
      setDraftBanned(detail.user.is_banned);
      setDraftBanNote(detail.ban_note);
      setDraftQuotas(
        Object.fromEntries(detail.quotas.map((quota) => [quota.root_domain, quota.effective_quota])),
      );
    } catch (loadError) {
      setError(loadError instanceof APIError ? loadError.message : '加载用户详情失败。');
    } finally {
      setEditingLoading(false);
    }
  }

  async function quickToggleBan(record: AdminUserRecord) {
    try {
      await updateAdminUser(
        record.id,
        { is_banned: !record.is_banned, ban_note: record.is_banned ? '' : '管理员在用户列表中快速封禁。' },
        csrfToken,
      );
      await loadUsers();
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '更新用户状态失败。');
    }
  }

  async function saveEditingRecord() {
    if (!editingDetail) {
      return;
    }

    try {
      setSaving(true);
      await updateAdminUser(
        editingDetail.user.id,
        { is_banned: draftBanned, ban_note: draftBanNote.trim() },
        csrfToken,
      );

      for (const quota of editingDetail.quotas) {
        const nextValue = draftQuotas[quota.root_domain];
        if (!Number.isFinite(nextValue) || nextValue === quota.effective_quota) {
          continue;
        }
        await setUserQuota(
          {
            username: editingDetail.user.username,
            root_domain: quota.root_domain,
            max_allocations: Math.max(1, Math.round(nextValue)),
            reason: 'admin-console',
          },
          csrfToken,
        );
      }

      await loadUsers();
      const refreshedDetail = await getAdminUserDetail(editingDetail.user.id);
      setEditingDetail(refreshedDetail);
      setDraftBanned(refreshedDetail.user.is_banned);
      setDraftBanNote(refreshedDetail.ban_note);
      setDraftQuotas(
        Object.fromEntries(refreshedDetail.quotas.map((quota) => [quota.root_domain, quota.effective_quota])),
      );
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '保存用户设置失败。');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-red-100 p-3 text-red-600 dark:bg-red-900/30 dark:text-red-300">
            <Users size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">用户管理</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">查看用户状态、信任等级与分配配额，并在必要时执行封禁。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索用户名、昵称或状态"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
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
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">用户</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">信任</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">状态</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">分配数</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">最近登录</th>
                <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={6} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                    正在加载用户列表...
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
                      <td className="px-5 py-4">
                        <div className="flex items-center gap-3">
                          {record.avatar_url ? (
                            <img src={record.avatar_url} alt={record.username} className="h-10 w-10 rounded-full object-cover" />
                          ) : (
                            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-slate-200 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                              <Shield size={16} />
                            </div>
                          )}
                          <div>
                            <div className="font-semibold text-slate-900 dark:text-white">{record.username}</div>
                            <div className="text-xs text-slate-500 dark:text-slate-400">{record.display_name}</div>
                          </div>
                        </div>
                      </td>
                      <td className="px-5 py-4 text-slate-600 dark:text-slate-300">TL {record.trust_level}</td>
                      <td className="px-5 py-4">
                        <span
                          className={`inline-flex rounded-full px-2.5 py-1 text-xs font-semibold ${
                            record.is_banned
                              ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
                              : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                          }`}
                        >
                          {record.is_banned ? '已封禁' : '正常'}
                        </span>
                      </td>
                      <td className="px-5 py-4 text-slate-500 dark:text-slate-400">{record.allocation_count}</td>
                      <td className="px-5 py-4 text-slate-500 dark:text-slate-400">{formatDateTime(record.last_login_at)}</td>
                      <td className="px-5 py-4">
                        <div className="flex justify-end gap-2">
                          <button
                            onClick={() => void openEditor(record)}
                            className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white"
                            aria-label={`编辑 ${record.username}`}
                          >
                            <Edit2 size={16} />
                          </button>
                          <button
                            onClick={() => void quickToggleBan(record)}
                            className={`rounded-xl p-2 transition ${
                              record.is_banned
                                ? 'text-emerald-500 hover:bg-emerald-100 dark:hover:bg-emerald-900/25'
                                : 'text-red-500 hover:bg-red-100 dark:hover:bg-red-900/25'
                            }`}
                            aria-label={record.is_banned ? `恢复 ${record.username}` : `封禁 ${record.username}`}
                          >
                            {record.is_banned ? <CheckCircle2 size={16} /> : <Ban size={16} />}
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

      {editingLoading ? (
        <div className="mt-5 rounded-2xl border border-slate-200/70 bg-white/60 px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-white/10 dark:bg-white/5 dark:text-slate-300">
          正在加载用户详情...
        </div>
      ) : null}

      {editingDetail ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
          <button className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={() => setEditingDetail(null)} aria-label="关闭编辑弹窗" />
          <GlassCard className="relative z-10 w-full max-w-2xl border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">编辑用户</h2>
            <div className="grid gap-5 lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
              <div className="space-y-4">
                <div>
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">用户名</label>
                  <input value={editingDetail.user.username} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
                </div>
                <div>
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">论坛昵称</label>
                  <input value={editingDetail.user.display_name} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">信任等级</label>
                    <input value={`TL ${editingDetail.user.trust_level}`} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
                  </div>
                  <div>
                    <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">最近登录</label>
                    <input value={formatDateTime(editingDetail.user.last_login_at)} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
                  </div>
                </div>
                <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                  <input type="checkbox" checked={draftBanned} onChange={(event) => setDraftBanned(event.target.checked)} />
                  立即封禁该账号
                </label>
                <div>
                  <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">封禁备注</label>
                  <textarea
                    value={draftBanNote}
                    onChange={(event) => setDraftBanNote(event.target.value)}
                    rows={4}
                    className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                  />
                </div>
              </div>

              <div>
                <div className="mb-3 flex items-center justify-between">
                  <div>
                    <div className="text-sm font-semibold text-slate-900 dark:text-white">域名配额</div>
                    <div className="text-xs text-slate-500 dark:text-slate-400">支持按根域名单独设置可分配数量。</div>
                  </div>
                  <div className="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                    已接入 {managedDomains.length} 个根域名
                  </div>
                </div>
                <div className="space-y-3">
                  {editingDetail.quotas.map((quota) => (
                    <div key={quota.root_domain} className="rounded-2xl border border-slate-200 bg-white/70 p-4 dark:border-slate-700 dark:bg-black/35">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <div className="font-semibold text-slate-900 dark:text-white">{quota.root_domain}</div>
                          <div className="text-xs text-slate-500 dark:text-slate-400">
                            默认 {quota.default_quota}，当前已使用 {quota.allocation_count}
                          </div>
                        </div>
                        <input
                          type="number"
                          min={1}
                          value={draftQuotas[quota.root_domain] ?? quota.effective_quota}
                          onChange={(event) =>
                            setDraftQuotas((current) => ({
                              ...current,
                              [quota.root_domain]: Math.max(1, Number(event.target.value) || 1),
                            }))
                          }
                          className="w-24 rounded-2xl border border-slate-200 bg-white/85 px-3 py-2 text-center outline-none focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-600 dark:bg-slate-900 dark:text-white"
                        />
                      </div>
                    </div>
                  ))}
                  {editingDetail.quotas.length === 0 ? (
                    <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
                      当前还没有可管理的根域名配置。
                    </div>
                  ) : null}
                </div>
              </div>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => setEditingDetail(null)} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">
                关闭
              </button>
              <button
                onClick={() => void saveEditingRecord()}
                disabled={saving}
                className="flex-1 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-4 py-3 font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
              >
                {saving ? '保存中...' : '保存'}
              </button>
            </div>
          </GlassCard>
        </div>
      ) : null}
    </div>
  );
}
