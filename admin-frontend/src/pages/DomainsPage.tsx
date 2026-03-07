import { useMemo, useState } from 'react';
import { Cloud, Edit2, Search, ShieldCheck, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { mockDomains } from '../data/mockAdminData';
import { GlassCard } from '../components/GlassCard';
import type { AdminDomainRecord } from '../types/admin';

// DomainsPage 展示管理员对 DNS 记录的查看与编辑原型。
export function DomainsPage() {
  const [records, setRecords] = useState<AdminDomainRecord[]>(mockDomains);
  const [keyword, setKeyword] = useState('');
  const [editingRecord, setEditingRecord] = useState<AdminDomainRecord | null>(null);

  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [record.owner, record.hostname, record.type, record.content].some((field) => field.toLowerCase().includes(search)),
    );
  }, [keyword, records]);

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
          <div className="rounded-2xl bg-blue-100 p-3 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300">
            <Cloud size={28} />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">域名管理</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">审阅解析记录、代理状态与所属用户，后续可替换为真实 DNS 管理 API。</p>
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
                <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">创建时间</th>
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
                    <td className="px-5 py-4 font-medium text-slate-900 dark:text-white">{record.owner}</td>
                    <td className="px-5 py-4 font-mono text-blue-600 dark:text-blue-300">{record.hostname}</td>
                    <td className="px-5 py-4"><span className="rounded-lg bg-slate-100 px-2 py-1 text-xs font-semibold dark:bg-slate-800">{record.type}</span></td>
                    <td className="px-5 py-4 font-mono text-slate-600 dark:text-slate-300">{record.content}</td>
                    <td className="px-5 py-4">
                      <span className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-semibold ${record.proxied ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                        <ShieldCheck size={12} />
                        {record.proxied ? '已开启' : '未开启'}
                      </span>
                    </td>
                    <td className="px-5 py-4 text-slate-500 dark:text-slate-400">{record.createdAt}</td>
                    <td className="px-5 py-4">
                      <div className="flex justify-end gap-2">
                        <button onClick={() => setEditingRecord({ ...record })} className="rounded-xl p-2 text-blue-500 transition hover:bg-blue-100 dark:hover:bg-blue-900/25" aria-label={`编辑 ${record.hostname}`}>
                          <Edit2 size={16} />
                        </button>
                        <button onClick={() => removeRecord(record.id)} className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white" aria-label={`删除 ${record.hostname}`}>
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
          <GlassCard className="relative z-10 w-full max-w-lg border-white/35 bg-white/80 p-6 dark:bg-slate-950/80">
            <h2 className="mb-5 text-2xl font-bold text-slate-900 dark:text-white">编辑解析记录</h2>
            <div className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">主机名</label>
                <input value={editingRecord.hostname} disabled className="w-full rounded-2xl border border-slate-200 bg-slate-100 px-4 py-3 text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400" />
              </div>
              <div>
                <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">记录内容</label>
                <input value={editingRecord.content} onChange={(event) => setEditingRecord({ ...editingRecord, content: event.target.value })} className="w-full rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
              </div>
              <label className="flex items-center gap-3 rounded-2xl border border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-700 dark:border-slate-700 dark:bg-black/35 dark:text-slate-200">
                <input type="checkbox" checked={editingRecord.proxied} onChange={(event) => setEditingRecord({ ...editingRecord, proxied: event.target.checked })} />
                通过 Cloudflare 代理访问
              </label>
            </div>
            <div className="mt-6 flex gap-3">
              <button onClick={() => setEditingRecord(null)} className="flex-1 rounded-2xl bg-slate-100 px-4 py-3 font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-100">取消</button>
              <button onClick={saveEditingRecord} className="flex-1 rounded-2xl bg-gradient-to-r from-blue-500 to-indigo-500 px-4 py-3 font-medium text-white">保存</button>
            </div>
          </GlassCard>
        </div>
      ) : null}
    </div>
  );
}
