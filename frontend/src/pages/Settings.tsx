import { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { GlassCard } from '../components/GlassCard';
import { GlassSelect, type GlassSelectOption } from '../components/GlassSelect';
import {
  Plus,
  Trash2,
  Edit2,
  X,
  LoaderCircle,
  ArrowRight,
  LogOut,
  Sparkles,
} from 'lucide-react';
import confetti from 'canvas-confetti';
import {
  APIError,
  createDNSRecord,
  deleteDNSRecord,
  listAllocationRecords,
  updateDNSRecord,
} from '../lib/api';
import type { Allocation, DNSRecord, ManualDNSRecordType, UpsertDNSRecordInput, User } from '../types/api';

// RecordFormState 表示 DNS 记录弹窗中的表单状态。
interface RecordFormState {
  type: ManualDNSRecordType;
  name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority: string;
}

// SettingsProps 描述配置中心需要的用户态、分配列表和操作回调。
interface SettingsProps {
  authenticated: boolean;
  sessionLoading: boolean;
  user?: User;
  allocations: Allocation[];
  csrfToken?: string;
  onLogin: () => void;
  onNavigateDomains: () => void;
  onSessionRefresh: () => Promise<void>;
  onLogout: () => Promise<void>;
}

// emptyForm 是 DNS 记录弹窗默认使用的初始表单值。
const emptyForm: RecordFormState = {
  type: 'A',
  name: '@',
  content: '',
  ttl: 1,
  proxied: true,
  comment: '',
  priority: '',
};

// dnsTypeOptions 统一定义 DNS 类型下拉选项，供自定义玻璃态选择器复用。
const dnsTypeOptions: GlassSelectOption[] = [
  { value: 'A', label: 'A' },
  { value: 'AAAA', label: 'AAAA' },
  { value: 'CNAME', label: 'CNAME' },
  { value: 'TXT', label: 'TXT' },
];

// Settings 负责接入用户自己的 allocation 和 DNS 记录管理能力。
export function Settings({
  authenticated,
  sessionLoading,
  user,
  allocations,
  csrfToken,
  onLogin,
  onNavigateDomains,
  onSessionRefresh,
  onLogout,
}: SettingsProps) {
  // selectedAllocationID 保存当前用户正在查看的命名空间。
  const [selectedAllocationID, setSelectedAllocationID] = useState<number | null>(null);

  // records 保存当前命名空间下的实时 DNS 记录列表。
  const [records, setRecords] = useState<DNSRecord[]>([]);

  // recordsLoading 用于控制 DNS 记录列表的加载状态。
  const [recordsLoading, setRecordsLoading] = useState(false);

  // recordsError 用于保存 DNS 记录列表加载失败的信息。
  const [recordsError, setRecordsError] = useState('');

  // notice 用于展示创建、更新、删除后的反馈信息。
  const [notice, setNotice] = useState('');

  // isModalOpen 控制 DNS 记录编辑弹窗的显示与隐藏。
  const [isModalOpen, setIsModalOpen] = useState(false);

  // editingRecord 表示当前正在编辑的记录；为空时表示新建模式。
  const [editingRecord, setEditingRecord] = useState<DNSRecord | null>(null);

  // formData 保存弹窗表单状态。
  const [formData, setFormData] = useState<RecordFormState>(emptyForm);

  // isSaving 用于控制保存按钮的提交中状态。
  const [isSaving, setIsSaving] = useState(false);

  // deletingRecordID 用于标记当前正在删除的记录。
  const [deletingRecordID, setDeletingRecordID] = useState<string | null>(null);

  // 当 allocation 列表更新时，自动选中 primary 或第一条 allocation。
  useEffect(() => {
    if (!authenticated || allocations.length === 0) {
      setSelectedAllocationID(null);
      setRecords([]);
      return;
    }

    const currentExists = allocations.some((item) => item.id === selectedAllocationID);
    if (currentExists) {
      return;
    }

    const preferred = allocations.find((item) => item.is_primary) ?? allocations[0];
    setSelectedAllocationID(preferred.id);
  }, [allocations, authenticated, selectedAllocationID]);

  // 当用户切换 allocation 时，读取该命名空间下的实时 DNS 记录。
  useEffect(() => {
    if (!authenticated || selectedAllocationID == null) {
      setRecords([]);
      return;
    }

    void loadRecords(selectedAllocationID);
  }, [authenticated, selectedAllocationID]);

  // selectedAllocation 方便模板层读取当前选中的 allocation。
  const selectedAllocation = allocations.find((item) => item.id === selectedAllocationID) ?? null;
  const primaryAllocation = allocations.find((item) => item.is_primary) ?? allocations[0] ?? null;
  const additionalAllocations = allocations.filter((item) => !item.is_primary);

  // loadRecords 从后端读取指定命名空间下的记录列表。
  async function loadRecords(allocationID: number): Promise<void> {
    setRecordsLoading(true);
    setRecordsError('');

    try {
      const nextRecords = await listAllocationRecords(allocationID);
      const selected = allocations.find((item) => item.id === allocationID);
      if (nextRecords.length === 0 && selected && user) {
        setRecords([buildPlaceholderRecord(selected)]);
        return;
      }
      setRecords(nextRecords);
    } catch (error) {
      setRecords([]);
      setRecordsError(readableErrorMessage(error, '无法加载当前命名空间的 DNS 记录'));
    } finally {
      setRecordsLoading(false);
    }
  }

  // triggerFireworks 继续沿用原有庆祝动画，在写操作成功后调用。
  function triggerFireworks(): void {
    const duration = 3 * 1000;
    const end = Date.now() + duration;

    const frame = () => {
      confetti({
        particleCount: 5,
        angle: 60,
        spread: 55,
        origin: { x: 0 },
        colors: ['#2dd4bf', '#34d399', '#0ea5e9'],
      });
      confetti({
        particleCount: 5,
        angle: 120,
        spread: 55,
        origin: { x: 1 },
        colors: ['#2dd4bf', '#34d399', '#0ea5e9'],
      });

      if (Date.now() < end) {
        requestAnimationFrame(frame);
      }
    };

    frame();
  }

  // openModal 打开记录弹窗，并根据是否传入 record 决定是编辑还是新建。
  function openModal(record?: DNSRecord): void {
    if (record && !isPlaceholderRecord(record) && supportsManualRecordType(record.type)) {
      setEditingRecord(record);
      setFormData({
        type: record.type,
        name: record.relative_name,
        content: record.content,
        ttl: record.ttl,
        proxied: record.proxied,
        comment: record.comment ?? '',
        priority: record.priority != null ? String(record.priority) : '',
      });
    } else {
      setEditingRecord(null);
      setFormData({
        ...emptyForm,
        name: '@',
      });
    }

    setNotice('');
    setIsModalOpen(true);
  }

  // closeModal 关闭弹窗并重置编辑态。
  function closeModal(): void {
    setIsModalOpen(false);
    setEditingRecord(null);
    setFormData(emptyForm);
  }

  // handleSave 把弹窗表单提交给后端，用于创建或更新 DNS 记录。
  async function handleSave(): Promise<void> {
    if (!selectedAllocation || !csrfToken) {
      setNotice('当前会话不可用，请刷新页面后重试。');
      return;
    }

    if (!formData.name.trim() || !formData.content.trim()) {
      setNotice('记录名称和内容不能为空。');
      return;
    }

    setIsSaving(true);
    setNotice('');

    try {
      const payload = buildRecordPayload(formData);

      if (editingRecord) {
        await updateDNSRecord(selectedAllocation.id, editingRecord.id, payload, csrfToken);
      } else {
        await createDNSRecord(selectedAllocation.id, payload, csrfToken);
      }

      await loadRecords(selectedAllocation.id);
      closeModal();
      setNotice(editingRecord ? '记录更新成功。' : '记录创建成功。');
      triggerFireworks();
    } catch (error) {
      setNotice(readableErrorMessage(error, '保存记录失败'));
    } finally {
      setIsSaving(false);
    }
  }

  // handleDelete 删除指定记录。
  async function handleDelete(recordID: string): Promise<void> {
    if (!selectedAllocation || !csrfToken) {
      setNotice('当前会话不可用，请刷新页面后重试。');
      return;
    }
    if (recordID.startsWith('placeholder:')) {
      setNotice('占位记录尚未创建到 Cloudflare，无需删除。');
      return;
    }

    setDeletingRecordID(recordID);
    setNotice('');

    try {
      await deleteDNSRecord(selectedAllocation.id, recordID, csrfToken);
      await loadRecords(selectedAllocation.id);
      setNotice('记录删除成功。');
    } catch (error) {
      setNotice(readableErrorMessage(error, '删除记录失败'));
    } finally {
      setDeletingRecordID(null);
    }
  }

  // 未登录时，配置中心显示登录引导卡片，而不是空白页。
  if (!authenticated) {
    return (
      <div className="max-w-4xl mx-auto pt-32 pb-24 px-6">
        <GlassCard className="text-center">
          <h1 className="text-3xl font-extrabold text-gray-900 dark:text-white">解析配置中心</h1>
          <p className="mt-3 text-gray-700 dark:text-gray-300">
            {sessionLoading ? '正在检查当前登录状态...' : '请先通过 Linux Do 登录，再管理你的 DNS 命名空间。'}
          </p>
          <div className="mt-6 flex flex-col sm:flex-row items-center justify-center gap-3">
            <button
              onClick={onLogin}
              className="px-6 py-3 rounded-xl bg-gradient-to-r from-teal-500 to-emerald-600 hover:from-teal-600 hover:to-emerald-700 text-white font-medium shadow-lg transition-all"
            >
              立即登录
            </button>
            <button
              onClick={onNavigateDomains}
              className="px-6 py-3 rounded-xl bg-white/45 dark:bg-black/30 hover:bg-white/60 dark:hover:bg-black/45 text-gray-900 dark:text-white font-medium transition-colors"
            >
              去域名分发页
            </button>
          </div>
        </GlassCard>
      </div>
    );
  }

  // 已登录但没有 allocation 时，提示用户先去申请命名空间。
  if (!sessionLoading && allocations.length === 0) {
    return (
      <div className="max-w-4xl mx-auto pt-32 pb-24 px-6">
        <GlassCard className="text-center">
          <h1 className="text-3xl font-extrabold text-gray-900 dark:text-white">解析配置中心</h1>
          <p className="mt-3 text-gray-700 dark:text-gray-300">
            {user?.display_name || user?.username}，你当前还没有可管理的命名空间。
          </p>
          <div className="mt-6 flex flex-col sm:flex-row items-center justify-center gap-3">
            <button
              onClick={onNavigateDomains}
              className="px-6 py-3 rounded-xl bg-gradient-to-r from-teal-500 to-emerald-600 hover:from-teal-600 hover:to-emerald-700 text-white font-medium shadow-lg transition-all inline-flex items-center gap-2"
            >
              <ArrowRight size={18} />
              前往申请域名
            </button>
            <button
              onClick={() => void onSessionRefresh()}
              className="px-6 py-3 rounded-xl bg-white/45 dark:bg-black/30 hover:bg-white/60 dark:hover:bg-black/45 text-gray-900 dark:text-white font-medium transition-colors"
            >
              刷新状态
            </button>
          </div>
        </GlassCard>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto pt-32 pb-24 px-6 relative">
      <motion.div
        initial={{ y: 20, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        className="mb-8 flex flex-col gap-4"
      >
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div>
            <h1 className="text-3xl font-extrabold text-gray-900 dark:text-white">解析配置中心</h1>
            <p className="text-gray-700 dark:text-gray-300 mt-2">
              管理你在 Cloudflare 上的真实 DNS 记录
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <button
              onClick={() => void onLogout()}
              className="flex items-center justify-center gap-2 px-5 py-3 rounded-xl bg-white/45 dark:bg-black/30 hover:bg-white/60 dark:hover:bg-black/45 text-gray-900 dark:text-white font-medium transition-colors"
            >
              <LogOut size={18} />
              退出
            </button>
            <button
              onClick={() => openModal()}
              disabled={!selectedAllocation}
              className="flex items-center justify-center gap-2 px-6 py-3 rounded-xl bg-gradient-to-r from-teal-500 to-emerald-600 hover:from-teal-600 hover:to-emerald-700 disabled:opacity-60 disabled:cursor-not-allowed text-white font-medium shadow-lg transition-all transform hover:scale-105"
            >
              <Plus size={18} />
              添加记录
            </button>
          </div>
        </div>

        <GlassCard className="p-5">
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
              <div>
                <div className="text-sm uppercase tracking-[0.22em] text-teal-600 dark:text-teal-300 font-bold">
                  Namespace Library
                </div>
                <div className="mt-2 text-xl font-extrabold text-gray-900 dark:text-white">
                  你当前共有 {allocations.length} 个可管理命名空间
                </div>
                <div className="mt-2 text-sm text-gray-600 dark:text-gray-300">
                  默认命名空间与管理员额外发放的命名空间都会出现在这里，你可以随时切换。
                </div>
              </div>
              {primaryAllocation ? (
                <div className="rounded-2xl bg-white/35 dark:bg-black/30 border border-white/20 px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
                  <div>默认命名空间：{primaryAllocation.fqdn}</div>
                  <div>额外命名空间：{Math.max(0, allocations.length - 1)} 个</div>
                </div>
              ) : null}
            </div>

            <div className="grid gap-3 xl:grid-cols-2">
              {allocations.map((allocation) => {
                const isSelected = selectedAllocationID === allocation.id;
                return (
                  <button
                    key={allocation.id}
                    type="button"
                    onClick={() => setSelectedAllocationID(allocation.id)}
                    className={`rounded-3xl border p-4 text-left transition-all ${
                      isSelected
                        ? 'border-teal-400/60 bg-gradient-to-r from-teal-500 to-emerald-500 text-white shadow-xl'
                        : 'border-white/20 bg-white/40 text-gray-800 hover:bg-white/60 dark:border-white/10 dark:bg-black/25 dark:text-gray-100 dark:hover:bg-black/35'
                    }`}
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${isSelected ? 'bg-white/20 text-white' : 'bg-teal-100 text-teal-700 dark:bg-teal-900/35 dark:text-teal-300'}`}>
                        {allocation.is_primary ? '默认命名空间' : '额外命名空间'}
                      </span>
                      <span className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${isSelected ? 'bg-white/15 text-white/90' : 'bg-white/70 text-gray-600 dark:bg-white/10 dark:text-gray-300'}`}>
                        {formatAllocationSource(allocation.source)}
                      </span>
                      <span className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${allocation.status === 'active' ? isSelected ? 'bg-emerald-400/25 text-white' : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/35 dark:text-emerald-300' : isSelected ? 'bg-white/15 text-white/90' : 'bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300'}`}>
                        {allocation.status === 'active' ? '启用中' : '已停用'}
                      </span>
                    </div>
                    <div className="mt-3 text-lg font-extrabold break-all">
                      {allocation.fqdn}
                    </div>
                    <div className={`mt-2 text-sm ${isSelected ? 'text-white/90' : 'text-gray-600 dark:text-gray-300'}`}>
                      根域名：{allocation.root_domain || allocation.fqdn.split('.').slice(1).join('.')}
                    </div>
                    <div className={`mt-1 text-sm ${isSelected ? 'text-white/90' : 'text-gray-600 dark:text-gray-300'}`}>
                      记录空间：支持 `@`、`www`、`api.v2` 等属于该命名空间的记录
                    </div>
                  </button>
                );
              })}
            </div>

            {additionalAllocations.length > 0 ? (
              <div className="rounded-2xl border border-sky-300/35 bg-sky-100/45 px-4 py-3 text-sm text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/25 dark:text-sky-200">
                管理员已为你额外分配 {additionalAllocations.length} 个命名空间。它们与默认同名子域一样，会在这里长期显示并可切换管理。
              </div>
            ) : null}

            {selectedAllocation && (
              <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                  <div className="text-sm uppercase tracking-[0.25em] text-teal-600 dark:text-teal-300 font-bold">
                    Active Namespace
                  </div>
                  <div className="mt-2 text-2xl font-extrabold text-gray-900 dark:text-white">
                    {selectedAllocation.fqdn}
                  </div>
                  <div className="mt-2 text-sm text-gray-600 dark:text-gray-300">
                    你现在可以管理 `@`、`www`、`api.v2` 等所有属于该命名空间的记录。
                  </div>
                </div>

                <div className="rounded-2xl bg-white/35 dark:bg-black/30 border border-white/20 px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
                  <div>来源：{formatAllocationSource(selectedAllocation.source)}</div>
                  <div>默认命名空间：{selectedAllocation.is_primary ? '是' : '否'}</div>
                  <div>状态：{selectedAllocation.status === 'active' ? '启用中' : '已停用'}</div>
                </div>
              </div>
            )}
          </div>
        </GlassCard>

        {notice && (
          <div className="rounded-2xl border border-teal-300/40 bg-teal-100/60 dark:bg-teal-950/25 dark:border-teal-700/40 px-4 py-3 text-sm text-teal-900 dark:text-teal-200">
            {notice}
          </div>
        )}

        {recordsError && (
          <div className="rounded-2xl border border-red-300/40 bg-red-100/60 dark:bg-red-950/25 dark:border-red-700/40 px-4 py-3 text-sm text-red-900 dark:text-red-200">
            {recordsError}
          </div>
        )}
      </motion.div>

      <GlassCard className="overflow-hidden p-0">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="border-b border-white/20 dark:border-white/10 bg-white/20 dark:bg-black/20">
                <th className="p-4 font-semibold text-gray-900 dark:text-white">类型</th>
                <th className="p-4 font-semibold text-gray-900 dark:text-white">名称</th>
                <th className="p-4 font-semibold text-gray-900 dark:text-white">内容</th>
                <th className="p-4 font-semibold text-gray-900 dark:text-white">代理状态</th>
                <th className="p-4 font-semibold text-gray-900 dark:text-white text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              <AnimatePresence>
                {records.map((record) => (
                  <motion.tr
                    key={record.id}
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: 'auto' }}
                    exit={{ opacity: 0, x: -50, backgroundColor: 'rgba(239, 68, 68, 0.2)' }}
                    transition={{ duration: 0.3 }}
                    className="border-b border-white/10 dark:border-white/5 hover:bg-white/30 dark:hover:bg-white/5 transition-colors"
                  >
                    <td className="p-4">
                      <span className="px-2 py-1 rounded-md bg-teal-100 dark:bg-teal-900/50 text-teal-700 dark:text-teal-300 text-sm font-bold">
                        {record.type}
                      </span>
                    </td>
                    <td className="p-4 font-medium text-gray-800 dark:text-gray-200">{record.relative_name}</td>
                    <td className="p-4 text-gray-600 dark:text-gray-400 font-mono text-sm">
                      <div>{record.content || (record.is_placeholder ? '尚未填写解析值' : '-')}</div>
                      {(record.comment || record.ttl || record.priority != null) && (
                        <div className="mt-1 text-xs text-gray-500 dark:text-gray-500">
                          TTL: {record.ttl === 1 ? 'Auto' : record.ttl}s
                          {record.priority != null ? ` · Priority: ${record.priority}` : ''}
                          {record.comment ? ` · ${record.comment}` : ''}
                        </div>
                      )}
                    </td>
                    <td className="p-4">
                      <div className="flex items-center gap-2">
                        <div
                          className={`w-3 h-3 rounded-full ${
                            record.proxied ? 'bg-orange-500 shadow-[0_0_8px_#f97316]' : 'bg-gray-400'
                          }`}
                        />
                        <span className="text-sm text-gray-700 dark:text-gray-300">
                          {record.proxied ? '已代理' : '仅 DNS'}
                        </span>
                      </div>
                    </td>
                    <td className="p-4 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => {
                            if (!supportsManualRecordType(record.type)) {
                              setNotice('MX 记录由系统托管，当前不能在解析面板中手动编辑。');
                              return;
                            }
                            openModal(record);
                          }}
                          disabled={!supportsManualRecordType(record.type)}
                          title={supportsManualRecordType(record.type) ? '编辑记录' : 'MX 记录由系统托管'}
                          className="p-2 rounded-lg hover:bg-white/50 dark:hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40 text-blue-600 dark:text-blue-400 transition-colors"
                        >
                          <Edit2 size={16} />
                        </button>
                        <button
                          onClick={() => void handleDelete(record.id)}
                          disabled={deletingRecordID === record.id || isPlaceholderRecord(record)}
                          className="p-2 rounded-lg hover:bg-white/50 dark:hover:bg-white/10 disabled:opacity-60 text-red-600 dark:text-red-400 transition-colors"
                        >
                          {deletingRecordID === record.id ? <LoaderCircle size={16} className="animate-spin" /> : <Trash2 size={16} />}
                        </button>
                      </div>
                    </td>
                  </motion.tr>
                ))}
              </AnimatePresence>
            </tbody>
          </table>
        </div>
        {recordsLoading && (
          <div className="p-12 text-center text-gray-500 dark:text-gray-400 flex items-center justify-center gap-3">
            <LoaderCircle size={18} className="animate-spin" />
            正在同步 Cloudflare DNS 记录...
          </div>
        )}
        {!recordsLoading && records.length === 0 && (
          <div className="p-12 text-center text-gray-500 dark:text-gray-400">
            当前命名空间还没有记录，点击上方按钮即可添加。
          </div>
        )}
      </GlassCard>

      <AnimatePresence>
        {isModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center px-4">
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="absolute inset-0 bg-black/40 backdrop-blur-sm"
              onClick={closeModal}
            />
            <motion.div
              initial={{ scale: 0.9, opacity: 0, y: 20 }}
              animate={{ scale: 1, opacity: 1, y: 0 }}
              exit={{ scale: 0.9, opacity: 0, y: 20 }}
              className="relative w-full max-w-md bg-white/80 dark:bg-gray-900/80 backdrop-blur-xl border border-white/20 dark:border-white/10 rounded-3xl p-6 shadow-2xl"
            >
              <button
                onClick={closeModal}
                className="absolute top-4 right-4 p-2 text-gray-500 hover:text-gray-900 dark:hover:text-white transition-colors"
              >
                <X size={20} />
              </button>

              <h2 className="text-2xl font-bold mb-6 text-gray-900 dark:text-white">
                {editingRecord ? '修改记录' : '添加记录'}
              </h2>

              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">记录类型</label>
                  <GlassSelect
                    value={formData.type}
                    options={dnsTypeOptions}
                    onChange={(value) =>
                      setFormData({
                        ...formData,
                        type: value as ManualDNSRecordType,
                        proxied: supportsProxy(value) ? formData.proxied : false,
                      })
                    }
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">名称</label>
                  <input
                    type="text"
                    value={formData.name}
                    onChange={(event) => setFormData({ ...formData, name: event.target.value })}
                    placeholder="@ 或 www"
                    className="w-full px-4 py-2 rounded-xl bg-white/50 dark:bg-black/50 border border-gray-200 dark:border-gray-700 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">内容</label>
                  <input
                    type="text"
                    value={formData.content}
                    onChange={(event) => setFormData({ ...formData, content: event.target.value })}
                    placeholder="IPv4 / IPv6 / 域名 / 文本"
                    className="w-full px-4 py-2 rounded-xl bg-white/50 dark:bg-black/50 border border-gray-200 dark:border-gray-700 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white"
                  />
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">TTL</label>
                    <input
                      type="number"
                      min={1}
                      value={formData.ttl}
                      onChange={(event) =>
                        setFormData({
                          ...formData,
                          ttl: Number(event.target.value || 1),
                        })
                      }
                      placeholder="1 = Auto"
                      className="w-full px-4 py-2 rounded-xl bg-white/50 dark:bg-black/50 border border-gray-200 dark:border-gray-700 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white"
                    />
                  </div>

                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">备注</label>
                  <input
                    type="text"
                    value={formData.comment}
                    onChange={(event) => setFormData({ ...formData, comment: event.target.value })}
                    placeholder="可选，用于标记用途"
                    className="w-full px-4 py-2 rounded-xl bg-white/50 dark:bg-black/50 border border-gray-200 dark:border-gray-700 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white"
                  />
                </div>

                <div className="flex items-center gap-3 pt-2">
                  <button
                    onClick={() =>
                      supportsProxy(formData.type) &&
                      setFormData({
                        ...formData,
                        proxied: !formData.proxied,
                      })
                    }
                    className={`relative w-12 h-6 rounded-full transition-colors ${
                      supportsProxy(formData.type)
                        ? formData.proxied
                          ? 'bg-orange-500'
                          : 'bg-gray-300 dark:bg-gray-600'
                        : 'bg-gray-200 dark:bg-gray-700 cursor-not-allowed opacity-60'
                    }`}
                  >
                    <div
                      className={`absolute top-1 left-1 w-4 h-4 rounded-full bg-white transition-transform ${
                        formData.proxied ? 'translate-x-6' : 'translate-x-0'
                      }`}
                    />
                  </button>
                  <span className="text-sm text-gray-700 dark:text-gray-300">
                    {supportsProxy(formData.type) ? '代理状态 (Cloudflare)' : '当前记录类型不支持代理'}
                  </span>
                </div>
              </div>

              <div className="mt-8 flex gap-3">
                <button
                  onClick={closeModal}
                  className="flex-1 px-4 py-2 rounded-xl bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-900 dark:text-white font-medium transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={() => void handleSave()}
                  disabled={isSaving}
                  className="flex-1 px-4 py-2 rounded-xl bg-gradient-to-r from-teal-500 to-emerald-600 hover:from-teal-600 hover:to-emerald-700 disabled:opacity-60 text-white font-medium shadow-lg transition-all flex items-center justify-center gap-2"
                >
                  {isSaving ? <LoaderCircle size={16} className="animate-spin" /> : <Sparkles size={16} />}
                  保存
                </button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
}

// buildRecordPayload 把表单状态转换为后端要求的请求结构。
function buildRecordPayload(formData: RecordFormState): UpsertDNSRecordInput {
  return {
    type: formData.type,
    name: formData.name,
    content: formData.content,
    ttl: Number.isFinite(formData.ttl) ? formData.ttl : 1,
    proxied: supportsProxy(formData.type) ? formData.proxied : false,
    comment: formData.comment,
  };
}

// supportsManualRecordType 判断某条记录是否仍允许通过用户 DNS 面板继续手动维护。
// 旧的 MX 记录即使暂时还能看到，也必须交给系统邮件中转逻辑托管。
function supportsManualRecordType(recordType: string): recordType is ManualDNSRecordType {
  return ['A', 'AAAA', 'CNAME', 'TXT'].includes(recordType.toUpperCase());
}

// supportsProxy 判断当前记录类型是否允许开启 Cloudflare 代理。
function supportsProxy(recordType: string): boolean {
  return ['A', 'AAAA', 'CNAME'].includes(recordType.toUpperCase());
}

// isPlaceholderRecord 判断某一行是否只是前端占位，不代表 Cloudflare 中已经存在真实记录。
function isPlaceholderRecord(record: DNSRecord): boolean {
  return record.is_placeholder === true;
}

// buildPlaceholderRecord 在真实记录为空时，为当前命名空间生成一条前端占位行。
function buildPlaceholderRecord(allocation: Allocation): DNSRecord {
  return {
    id: `placeholder:${allocation.id}`,
    type: 'CNAME',
    name: allocation.fqdn,
    relative_name: '@',
    content: '',
    ttl: 1,
    proxied: true,
    comment: `${allocation.fqdn} 的占位记录，表示当前命名空间尚未写入真实解析值`,
    is_placeholder: true,
  };
}

// formatAllocationSource 将后端来源标记转换成更适合用户阅读的文案。
function formatAllocationSource(source: string): string {
  const normalizedSource = source.trim().toLowerCase();
  switch (normalizedSource) {
    case 'auto_provision':
      return '自动发放';
    case 'manual':
      return '手动申请';
    case 'admin_grant':
      return '管理员发放';
    default:
      return source.trim() || '未标记来源';
  }
}

// readableErrorMessage 统一提取接口错误文本。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
