import { useState, type FormEvent } from 'react';
import { CheckCircle2, Copy, Plus, Ticket, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { mockRedeemCodes } from '../data/mockAdminData';
import { GlassCard } from '../components/GlassCard';
import type { AdminRedeemCodeRecord, RedeemPermissionType } from '../types/admin';

// RedeemCodesPage 提供兑换码生成、复制和删除的前端演示能力。
export function RedeemCodesPage() {
  const [records, setRecords] = useState<AdminRedeemCodeRecord[]>(mockRedeemCodes);
  const [amount, setAmount] = useState(1);
  const [permissionType, setPermissionType] = useState<RedeemPermissionType>('single');
  const [target, setTarget] = useState('');

  function readableType(type: RedeemPermissionType): string {
    switch (type) {
      case 'wildcard':
        return '泛解析';
      case 'multiple':
        return '追加额度';
      case 'single':
      default:
        return '特定域名';
    }
  }

  // handleGenerate 在本地生成演示兑换码，便于先还原页面交互。
  function handleGenerate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextRecords = Array.from({ length: amount }).map((_, index) => ({
      id: Date.now() + index,
      code: `LINUXDO-${new Date().getFullYear()}-${Math.random().toString(36).slice(2, 8).toUpperCase()}`,
      type: permissionType,
      target: target.trim() || (permissionType === 'multiple' ? '默认额度' : '待填写目标'),
      usedBy: null,
      createdAt: new Date().toISOString().slice(0, 10),
    }));

    setRecords((current) => [...nextRecords, ...current]);
    setAmount(1);
    setTarget('');
  }

  async function copyCode(code: string) {
    await navigator.clipboard.writeText(code);
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex items-center gap-3">
        <div className="rounded-2xl bg-indigo-100 p-3 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300">
          <Ticket size={28} />
        </div>
        <div>
          <h1 className="text-3xl font-bold text-slate-900 dark:text-white">兑换码</h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">生成临时授权码，后续可接入后台签发与核销逻辑。</p>
        </div>
      </div>

      <div className="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]">
        <GlassCard>
          <form onSubmit={handleGenerate} className="space-y-4">
            <h2 className="flex items-center gap-2 text-xl font-bold text-slate-900 dark:text-white">
              <Plus size={18} className="text-indigo-500" />
              批量生成兑换码
            </h2>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">生成数量</label>
              <input type="number" min={1} max={100} value={amount} onChange={(event) => setAmount(Math.max(1, Number(event.target.value) || 1))} className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">授权类型</label>
              <select value={permissionType} onChange={(event) => setPermissionType(event.target.value as RedeemPermissionType)} className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white">
                <option value="single">特定域名</option>
                <option value="multiple">追加额度</option>
                <option value="wildcard">泛解析</option>
              </select>
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">授权目标</label>
              <input value={target} onChange={(event) => setTarget(event.target.value)} placeholder={permissionType === 'multiple' ? '例如 5 次额度' : '例如 api.linuxdo.space'} className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
            </div>

            <button type="submit" className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-indigo-500 to-violet-500 px-4 py-3 font-medium text-white shadow-lg transition hover:from-indigo-600 hover:to-violet-600">
              <Ticket size={18} />
              <span>立即生成</span>
            </button>
          </form>
        </GlassCard>

        <GlassCard className="overflow-hidden p-0">
          <div className="custom-scrollbar overflow-x-auto">
            <table className="min-w-full border-collapse text-left">
              <thead>
                <tr className="border-b border-white/20 bg-white/20 dark:border-white/10 dark:bg-white/5">
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">兑换码</th>
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">权限内容</th>
                  <th className="px-5 py-4 text-sm font-semibold text-slate-900 dark:text-white">状态</th>
                  <th className="px-5 py-4 text-right text-sm font-semibold text-slate-900 dark:text-white">操作</th>
                </tr>
              </thead>
              <tbody>
                <AnimatePresence>
                  {records.map((record) => (
                    <motion.tr key={record.id} layout initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, x: -30 }} className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                      <td className="px-5 py-4">
                        <div className="font-mono font-semibold text-indigo-600 dark:text-indigo-300">{record.code}</div>
                        <div className="mt-1 text-xs text-slate-400">{record.createdAt}</div>
                      </td>
                      <td className="px-5 py-4">
                        <div className="font-medium text-slate-900 dark:text-white">{readableType(record.type)}</div>
                        <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{record.target}</div>
                      </td>
                      <td className="px-5 py-4">
                        {record.usedBy ? (
                          <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                            <CheckCircle2 size={12} />
                            已使用 ({record.usedBy})
                          </span>
                        ) : (
                          <span className="inline-flex rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-semibold text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                            未使用
                          </span>
                        )}
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex justify-end gap-2">
                          <button onClick={() => void copyCode(record.code)} className="rounded-xl p-2 text-indigo-500 transition hover:bg-indigo-100 dark:hover:bg-indigo-900/25" aria-label={`复制 ${record.code}`}><Copy size={16} /></button>
                          <button onClick={() => setRecords((current) => current.filter((item) => item.id !== record.id))} className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white" aria-label={`删除 ${record.code}`}><Trash2 size={16} /></button>
                        </div>
                      </td>
                    </motion.tr>
                  ))}
                </AnimatePresence>
              </tbody>
            </table>
          </div>
        </GlassCard>
      </div>
    </div>
  );
}
