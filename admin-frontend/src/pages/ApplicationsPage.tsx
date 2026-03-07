import { useMemo, useState } from 'react';
import { CheckCircle2, FileText, Search, XCircle } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { mockApplications } from '../data/mockAdminData';
import { GlassCard } from '../components/GlassCard';
import type { AdminApplicationRecord, ApplicationStatus } from '../types/admin';

// ApplicationsPage 用于演示特批申请的审核流转界面。
export function ApplicationsPage() {
  const [records, setRecords] = useState<AdminApplicationRecord[]>(mockApplications);
  const [keyword, setKeyword] = useState('');

  const filteredRecords = useMemo(() => {
    const search = keyword.trim().toLowerCase();
    if (!search) {
      return records;
    }
    return records.filter((record) =>
      [record.applicant, record.target, record.reason, record.status].some((field) => field.toLowerCase().includes(search)),
    );
  }, [keyword, records]);

  function updateStatus(id: number, status: ApplicationStatus) {
    setRecords((current) => current.map((record) => (record.id === id ? { ...record, status } : record)));
  }

  function typeLabel(type: AdminApplicationRecord['type']): string {
    switch (type) {
      case 'single':
        return '特定二级域名';
      case 'wildcard':
        return '泛解析';
      case 'multiple':
      default:
        return '追加额度';
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
            <h1 className="text-3xl font-bold text-slate-900 dark:text-white">权限申请</h1>
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">审核用户对特定域名、泛解析或额外额度的申请。</p>
          </div>
        </div>

        <label className="relative block w-full sm:w-80">
          <Search size={18} className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="搜索申请用户、目标或原因"
            className="w-full rounded-2xl border border-slate-200 bg-white/55 py-3 pl-11 pr-4 text-slate-900 outline-none transition focus:border-amber-400 focus:ring-2 focus:ring-amber-400/20 dark:border-slate-700 dark:bg-black/30 dark:text-white"
          />
        </label>
      </div>

      <div className="space-y-5">
        <AnimatePresence>
          {filteredRecords.map((record) => (
            <motion.div key={record.id} layout initial={{ opacity: 0, y: 14 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, scale: 0.97 }}>
              <GlassCard className="p-6">
                <div className="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
                  <div className="space-y-4">
                    <div className="flex flex-wrap items-center gap-3">
                      <span className="text-lg font-bold text-slate-900 dark:text-white">{record.applicant}</span>
                      <span className="rounded-full bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">{record.appliedAt}</span>
                      <span className={`rounded-full px-3 py-1 text-xs font-semibold ${record.status === 'pending' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' : record.status === 'approved' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'}`}>
                        {record.status === 'pending' ? '待审核' : record.status === 'approved' ? '已通过' : '已拒绝'}
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
                    </div>

                    <div>
                      <div className="mb-2 text-xs uppercase tracking-[0.24em] text-slate-400">申请理由</div>
                      <div className="rounded-2xl border border-white/20 bg-white/35 px-4 py-4 text-sm leading-6 text-slate-700 dark:border-white/10 dark:bg-black/25 dark:text-slate-200">
                        {record.reason}
                      </div>
                    </div>
                  </div>

                  {record.status === 'pending' ? (
                    <div className="flex gap-3 lg:flex-col">
                      <button onClick={() => updateStatus(record.id, 'approved')} className="flex items-center justify-center gap-2 rounded-2xl bg-emerald-500 px-5 py-3 text-sm font-medium text-white shadow-lg transition hover:bg-emerald-600">
                        <CheckCircle2 size={18} />
                        <span>批准</span>
                      </button>
                      <button onClick={() => updateStatus(record.id, 'rejected')} className="flex items-center justify-center gap-2 rounded-2xl bg-red-500 px-5 py-3 text-sm font-medium text-white shadow-lg transition hover:bg-red-600">
                        <XCircle size={18} />
                        <span>拒绝</span>
                      </button>
                    </div>
                  ) : null}
                </div>
              </GlassCard>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </div>
  );
}
