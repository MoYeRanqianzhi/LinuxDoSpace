import { useMemo, useState } from 'react';
import { Ban, CheckCircle2, Edit2, Search, Trash2, Users } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { mockUsers } from '../data/mockAdminData';
import { GlassCard } from '../components/GlassCard';
import type { AdminUserRecord, UserStatus } from '../types/admin';

// UsersPage 复用设计稿中的用户管理视图，并补上搜索和本地编辑状态。
export function UsersPage() {
  const [records, setRecords] = useState<AdminUserRecord[]>(mockUsers);
  const [keyword, setKeyword] = useState('');
  const [editingRecord, setEditingRecord] = useState<AdminUserRecord | null>(null);

  // filteredRecords 让搜索同时支持用户名、邮箱和状态。
  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [record.username, record.email, record.status].some((field) => field.toLowerCase().includes(search)),
    );
  }, [keyword, records]);

  function updateStatus(id: number, status: UserStatus) {
    setRecords((current) => current.map((record) => (record.id === id ? { ...record, status } : record)));
  }

  function removeRecord(id: number) {
    setRecords((current) => current.filter((record) => record.id !== id));
  }

  function saveEditingRecord() {
    if (!editingRecord) {
      return;
    }
    setRecords((current) => current.map((record) => (record.id === editingRecord.id ? editingRecord : record)));
    setEditingRecord(null);
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
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">查看账号状态，快速处理封禁与基础资料调整。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索用户名、邮箱或状态"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      <GlassCard className="overflow-hidden p-0">
        <div className="custom-scrollbar overflow-x-auto">
          <table className="min-w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-white/5">
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">ID</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">用户名</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">邮箱</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">状态</th>
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">注册时间</th>
                <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
              </tr>
            </thead>
            <tbody>
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
                    <td className="px-5 py-4 text-slate-500 dark:text-slate-400">#{record.id}</td>
                    <td className="px-5 py-4 font-semibold text-slate-900 dark:text-white">{record.username}</td>
                    <td className="px-5 py-4 text-slate-600 dark:text-slate-300">{record.email}</td>
                    <td className="px-5 py-4">
                      <span
                        className={`inline-flex rounded-full px-2.5 py-1 text-xs font-semibold ${
                          record.status === 'active'
                            ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                            : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
                        }`}
                      >
                        {record.status === 'active' ? '正常' : '已封禁'}
                      </span>
                    </td>
                    <td className="px-5 py-4 text-slate-500 dark:text-slate-400">{record.registeredAt}</td>
                    <td className="px-5 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => setEditingRecord({ ...record })}
                          className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white"
                          aria-label={`编辑 ${record.username}`}
                        >
                          <Edit2 size={16} />
                        </button>
                        {record.status === 'active' ? (
                          <button
                            onClick={() => updateStatus(record.id, 'banned')}
                            className="rounded-xl p-2 text-red-500 transition hover:bg-red-100 dark:hover:bg-red-900/25"
                            aria-label={`封禁 ${record.username}`}
                          >
                            <Ban size={16} />
                          </button>
                        ) : (
                          <button
                            onClick={() => updateStatus(record.id, 'active')}
                            className="rounded-xl p-2 text-emerald-500 transition hover:bg-emerald-100 dark:hover:bg-emerald-900/25"
                            aria-label={`恢复 ${record.username}`}
                          >
                            <CheckCircle2 size={16} />
                          </button>
                        )}
                        <button
                          onClick={() => removeRecord(record.id)}
                          className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white"
                          aria-label={`删除 ${record.username}`}
                        >
                          <Trash2 size={16} />
                        </button>
                      </div>
                    </td>
                  </motion.tr>
                ))}
              </AnimatePresence>
            </tbody>
          </table>
        </div>
      </GlassCard>

      {editingRecord ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
          <button className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={() => setEditingRecord(null)} aria-label="关闭编辑弹窗" />
          <GlassCard className="relative z-10 w-full max-w-md border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">编辑用户</h2>
            <div className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">用户名</label>
                <input
                  value={editingRecord.username}
                  onChange={(event) => setEditingRecord({ ...editingRecord, username: event.target.value })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">邮箱</label>
                <input
                  value={editingRecord.email}
                  onChange={(event) => setEditingRecord({ ...editingRecord, email: event.target.value })}
                  className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-red-400 focus:ring-2 focus:ring-red-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white"
                />
              </div>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => setEditingRecord(null)} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">
                取消
              </button>
              <button onClick={saveEditingRecord} className="flex-1 rounded-2xl bg-gradient-to-r from-red-500 to-orange-500 px-4 py-3 font-medium text-white">
                保存
              </button>
            </div>
          </GlassCard>
        </div>
      ) : null}
    </div>
  );
}
