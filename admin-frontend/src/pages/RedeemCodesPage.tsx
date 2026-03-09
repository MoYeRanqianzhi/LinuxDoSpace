import { useEffect, useState, type FormEvent } from 'react';
import { CheckCircle2, Copy, Plus, Ticket, Trash2 } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { APIError, deleteRedeemCode, generateRedeemCodes, listRedeemCodes } from '../lib/api';
import { AdminSelect } from '../components/AdminSelect';
import { GlassCard } from '../components/GlassCard';
import type { AdminRedeemCodeRecord, GenerateRedeemCodesInput, RedeemPermissionType } from '../types/admin';

interface RedeemCodesPageProps {
  csrfToken: string;
}

export function RedeemCodesPage({ csrfToken }: RedeemCodesPageProps) {
  const [records, setRecords] = useState<AdminRedeemCodeRecord[]>([]);
  const [amount, setAmount] = useState(1);
  const [permissionType, setPermissionType] = useState<RedeemPermissionType>('single');
  const [target, setTarget] = useState('');
  const [note, setNote] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    async function loadData() {
      try {
        setLoading(true);
        const data = await listRedeemCodes();
        setRecords(data);
        setError('');
      } catch (loadError) {
        setError(loadError instanceof APIError ? loadError.message : '加载兑换码失败。');
      } finally {
        setLoading(false);
      }
    }

    void loadData();
  }, []);

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

  async function handleGenerate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      setSaving(true);
      const created = await generateRedeemCodes(
        { amount, type: permissionType, target: target.trim(), note: note.trim() } as GenerateRedeemCodesInput,
        csrfToken,
      );
      setRecords((current) => [...created, ...current]);
      setAmount(1);
      setTarget('');
      setNote('');
      setError('');
    } catch (saveError) {
      setError(saveError instanceof APIError ? saveError.message : '生成兑换码失败。');
    } finally {
      setSaving(false);
    }
  }

  async function copyCode(code: string) {
    await navigator.clipboard.writeText(code);
  }

  async function removeCode(id: number) {
    try {
      await deleteRedeemCode(id, csrfToken);
      setRecords((current) => current.filter((record) => record.id !== id));
    } catch (deleteError) {
      setError(deleteError instanceof APIError ? deleteError.message : '删除兑换码失败。');
    }
  }

  return (
    <div className="mx-auto max-w-7xl">
      <div className="mb-8 flex items-center gap-3">
        <div className="rounded-2xl bg-indigo-100 p-3 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300">
          <Ticket size={28} />
        </div>
        <div>
          <h1 className="text-3xl font-bold text-slate-900 dark:text-white">兑换码</h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-300">批量生成一次性授权码，用于后续额外权限或额度发放。</p>
        </div>
      </div>

      {error ? (
        <div className="mb-5 rounded-2xl border border-red-300/50 bg-red-50/80 px-4 py-3 text-sm text-red-700 dark:border-red-500/20 dark:bg-red-950/30 dark:text-red-200">
          {error}
        </div>
      ) : null}

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
              <AdminSelect value={permissionType} onChange={(event) => setPermissionType(event.target.value as RedeemPermissionType)} className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white">
                <option value="single">特定域名</option>
                <option value="multiple">追加额度</option>
                <option value="wildcard">泛解析</option>
              </AdminSelect>
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">授权目标</label>
              <input value={target} onChange={(event) => setTarget(event.target.value)} placeholder={permissionType === 'multiple' ? '例如 5 次额度' : '例如 api.linuxdo.space'} className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-slate-700 dark:text-slate-200">备注</label>
              <input value={note} onChange={(event) => setNote(event.target.value)} placeholder="可选，用于后台追踪发放原因" className="w-full rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-400/20 dark:border-slate-700 dark:bg-black/35 dark:text-white" />
            </div>

            <button type="submit" disabled={saving || !target.trim()} className="flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-indigo-500 to-violet-500 px-4 py-3 font-medium text-white shadow-lg transition hover:from-indigo-600 hover:to-violet-600 disabled:cursor-not-allowed disabled:opacity-60">
              <Ticket size={18} />
              <span>{saving ? '生成中...' : '立即生成'}</span>
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
                {loading ? (
                  <tr>
                    <td colSpan={4} className="px-5 py-8 text-center text-sm text-slate-500 dark:text-slate-300">
                      正在加载兑换码...
                    </td>
                  </tr>
                ) : null}
                {!loading ? (
                  <AnimatePresence>
                    {records.map((record) => (
                      <motion.tr key={record.id} layout initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, x: -30 }} className="border-b border-white/10 text-sm hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5">
                        <td className="px-5 py-4">
                          <div className="font-mono font-semibold text-indigo-600 dark:text-indigo-300">{record.code}</div>
                          <div className="mt-1 text-xs text-slate-400">{new Date(record.created_at).toLocaleString('zh-CN')}</div>
                        </td>
                        <td className="px-5 py-4">
                          <div className="font-medium text-slate-900 dark:text-white">{readableType(record.type)}</div>
                          <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{record.target}</div>
                        </td>
                        <td className="px-5 py-4">
                          {record.used_by_username ? (
                            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                              <CheckCircle2 size={12} />
                              已使用 ({record.used_by_username})
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
                            <button onClick={() => void removeCode(record.id)} className="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-white/10 dark:hover:text-white" aria-label={`删除 ${record.code}`}><Trash2 size={16} /></button>
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
    </div>
  );
}
