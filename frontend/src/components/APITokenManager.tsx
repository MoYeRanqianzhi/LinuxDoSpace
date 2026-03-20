import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { ChevronDown, Copy, KeyRound, LoaderCircle, Plus, Trash2, X } from 'lucide-react';
import { GlassCard } from './GlassCard';
import { APIError, createMyAPIToken, listMyAPITokens, revokeMyAPIToken } from '../lib/api';
import type { UserAPIToken } from '../types/api';

// compactPreviewCount 控制折叠态下最多展示多少条 TOKEN 摘要。
// 这样可以保留“我确实已有 TOKEN”的反馈，但避免把高级功能铺满整个配置页。
const compactPreviewCount = 2;

// APITokenManagerProps 描述通用 TOKEN 管理卡片的输入契约。
// className 允许设置页把它放到页面底部时继续复用统一间距。
interface APITokenManagerProps {
  csrfToken?: string;
  className?: string;
}

// NoticeTone 统一描述当前操作反馈的语气。
type NoticeTone = 'error' | 'success' | 'info';

// SectionNotice 用于渲染组件内的反馈条。
interface SectionNotice {
  tone: NoticeTone;
  message: string;
}

// APITokenManager 负责管理通用 API TOKEN。
// 设计上它属于高级功能，所以默认收起创建表单和完整列表，只保留摘要与入口按钮。
export function APITokenManager({ csrfToken, className = '' }: APITokenManagerProps) {
  // apiTokens 保存后端返回的全部 TOKEN。
  const [apiTokens, setApiTokens] = useState<UserAPIToken[]>([]);

  // loading 控制首次读取与后续异步写操作后的同步状态。
  const [loading, setLoading] = useState(true);

  // tokenError 保存列表读取失败消息。
  const [tokenError, setTokenError] = useState('');

  // tokenNotice 保存创建、复制、撤销等操作反馈。
  const [tokenNotice, setTokenNotice] = useState<SectionNotice | null>(null);

  // newTokenName 保存创建表单中的 TOKEN 名称。
  const [newTokenName, setNewTokenName] = useState('');

  // creatingToken 控制创建提交按钮的 loading 态。
  const [creatingToken, setCreatingToken] = useState(false);

  // createdTokenSecret 保存创建成功后一次性返回的原始 TOKEN。
  const [createdTokenSecret, setCreatedTokenSecret] = useState('');

  // revokingTokenPublicIDs 逐行记录哪些 TOKEN 正在撤销。
  const [revokingTokenPublicIDs, setRevokingTokenPublicIDs] = useState<Record<string, boolean>>({});

  // isCreateFormOpen 控制“创建 TOKEN”表单是否展开。
  const [isCreateFormOpen, setIsCreateFormOpen] = useState(false);

  // isListExpanded 控制完整 TOKEN 列表是否展开。
  const [isListExpanded, setIsListExpanded] = useState(false);

  // activeTokenCount 统计当前未撤销的 TOKEN 数量。
  const activeTokenCount = useMemo(
    () => apiTokens.filter((item) => !item.revoked_at).length,
    [apiTokens],
  );

  // compactPreviewTokens 保存折叠态下展示的少量摘要行。
  const compactPreviewTokens = useMemo(
    () => apiTokens.slice(0, compactPreviewCount),
    [apiTokens],
  );

  // 组件挂载时读取一次 TOKEN 列表。
  useEffect(() => {
    void loadTokens();
  }, []);

  // loadTokens 从后端读取当前用户的 TOKEN 列表。
  // 本组件不再提供显式“刷新”按钮，而是由挂载和写操作后的自动同步负责。
  async function loadTokens(): Promise<void> {
    setLoading(true);
    try {
      const items = await listMyAPITokens();
      setApiTokens(items);
      setTokenError('');
    } catch (error) {
      const maybeTokenError = error;
      if (maybeTokenError instanceof APIError && maybeTokenError.code === 'not_found') {
        setApiTokens([]);
        setTokenError('');
      } else {
        setApiTokens([]);
        setTokenError(readableErrorMessage(error, '无法加载我的 API TOKEN 列表。'));
      }
    } finally {
      setLoading(false);
    }
  }

  // handleCreateToken 创建新的 TOKEN，并在成功后自动收起创建表单。
  async function handleCreateToken(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!csrfToken) {
      setTokenNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    const tokenName = newTokenName.trim();
    if (!tokenName) {
      setTokenNotice({ tone: 'error', message: '请输入 TOKEN 名称。' });
      return;
    }

    try {
      setCreatingToken(true);
      setTokenNotice(null);
      const result = await createMyAPIToken({ name: tokenName, email_enabled: true }, csrfToken);
      setApiTokens((currentItems) => upsertAPIToken(currentItems, result.token));
      setCreatedTokenSecret(result.raw_token);
      setNewTokenName('');
      setIsCreateFormOpen(false);
      setIsListExpanded(true);
      setTokenNotice({
        tone: 'success',
        message: `TOKEN ${result.token.name} 已创建。请立即复制保存原始密钥，离开当前提示后将无法再次查看。`,
      });
    } catch (error) {
      setTokenNotice({ tone: 'error', message: readableErrorMessage(error, '创建 API TOKEN 失败。') });
    } finally {
      setCreatingToken(false);
    }
  }

  // handleRevokeToken 撤销指定 TOKEN，并把结果回写到当前列表。
  async function handleRevokeToken(publicID: string): Promise<void> {
    if (!csrfToken) {
      setTokenNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    try {
      setRevokingTokenPublicIDs((current) => ({ ...current, [publicID]: true }));
      const item = await revokeMyAPIToken(publicID, csrfToken);
      setApiTokens((currentItems) => upsertAPIToken(currentItems, item));
      setTokenNotice({ tone: 'info', message: `TOKEN ${item.name} 已撤销。` });
    } catch (error) {
      setTokenNotice({ tone: 'error', message: readableErrorMessage(error, '撤销 API TOKEN 失败。') });
    } finally {
      setRevokingTokenPublicIDs((current) => {
        const next = { ...current };
        delete next[publicID];
        return next;
      });
    }
  }

  // handleCopyCreatedToken 复制一次性展示的原始 TOKEN。
  async function handleCopyCreatedToken(): Promise<void> {
    if (!createdTokenSecret) {
      return;
    }
    try {
      await navigator.clipboard.writeText(createdTokenSecret);
      setTokenNotice({ tone: 'success', message: 'TOKEN 已复制到剪贴板。' });
    } catch {
      setTokenNotice({ tone: 'info', message: '浏览器未允许自动复制，请手动复制下方原始 TOKEN。' });
    }
  }

  return (
    <GlassCard className={`space-y-4 ${className}`.trim()}>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex items-start gap-3">
          <div className="rounded-2xl bg-violet-500/15 p-3 text-violet-700 dark:text-violet-300">
            <KeyRound size={18} />
          </div>
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-bold text-gray-900 dark:text-white">API TOKEN</h2>
              <span className="rounded-full bg-violet-100 px-2.5 py-1 text-[11px] font-semibold text-violet-700 dark:bg-violet-900/35 dark:text-violet-300">
                高级功能
              </span>
            </div>
            <p className="mt-1 text-sm leading-7 text-gray-600 dark:text-gray-300">
              用于 SDK 或 API 客户端访问。它不是常规解析配置的一部分，所以默认只显示摘要；具体权限范围由 TOKEN 自身权限配置决定。
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {apiTokens.length > 0 ? (
            <button
              type="button"
              onClick={() => setIsListExpanded((current) => !current)}
              className="inline-flex items-center gap-2 rounded-2xl border border-white/20 bg-white/55 px-4 py-3 text-sm font-semibold text-gray-800 transition hover:bg-white/70 dark:border-white/10 dark:bg-black/30 dark:text-gray-100 dark:hover:bg-black/40"
            >
              <ChevronDown
                size={16}
                className={`transition-transform ${isListExpanded ? 'rotate-180' : ''}`}
              />
              {isListExpanded ? '收起 TOKEN 列表' : `查看 ${apiTokens.length} 个 TOKEN`}
            </button>
          ) : null}

          <button
            type="button"
            onClick={() => {
              setCreatedTokenSecret('');
              setTokenNotice(null);
              setIsCreateFormOpen((current) => !current);
            }}
            className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-4 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700"
          >
            {isCreateFormOpen ? <X size={16} /> : <Plus size={16} />}
            {isCreateFormOpen ? '收起创建' : '创建 TOKEN'}
          </button>
        </div>
      </div>

      <div className="rounded-2xl border border-white/15 bg-white/35 px-4 py-3 text-sm text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
        {loading ? '正在读取 TOKEN 摘要...' : `当前共有 ${apiTokens.length} 个 TOKEN，其中 ${activeTokenCount} 个未撤销。`}
      </div>

      {tokenError ? <InlineNotice tone="error" message={`TOKEN 列表加载失败：${tokenError}`} /> : null}
      {tokenNotice ? <InlineNotice tone={tokenNotice.tone} message={tokenNotice.message} /> : null}

      {createdTokenSecret ? (
        <div className="rounded-3xl border border-violet-300/35 bg-violet-50/80 p-5 dark:border-violet-700/35 dark:bg-violet-950/20">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <div className="text-sm font-semibold text-violet-900 dark:text-violet-100">新 TOKEN 原始密钥</div>
              <div className="mt-2 text-sm leading-7 text-violet-900/80 dark:text-violet-100/90">
                这串原始 TOKEN 只会展示这一次。请立即复制保存，之后页面只会保留公开 ID 和名称，不会再次返回原始密钥。
              </div>
            </div>
            <button
              type="button"
              onClick={() => void handleCopyCreatedToken()}
              className="inline-flex items-center gap-2 rounded-2xl bg-violet-600 px-4 py-3 text-sm font-semibold text-white transition hover:bg-violet-700"
            >
              <Copy size={16} />
              复制 TOKEN
            </button>
          </div>
          <div className="mt-4 rounded-2xl border border-violet-200/70 bg-white/75 px-4 py-3 font-mono text-sm break-all text-violet-900 dark:border-violet-700/35 dark:bg-black/25 dark:text-violet-100">
            {createdTokenSecret}
          </div>
        </div>
      ) : null}

      {isCreateFormOpen ? (
        <form className="space-y-4 rounded-3xl border border-white/15 bg-white/30 p-5 dark:border-white/10 dark:bg-black/15" onSubmit={(event) => void handleCreateToken(event)}>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">TOKEN 名称</label>
            <div className="mt-2 flex min-w-0 items-center rounded-2xl border border-white/20 bg-white/55 px-4 py-3 shadow-inner dark:border-white/10 dark:bg-black/35">
              <input
                type="text"
                value={newTokenName}
                onChange={(event) => setNewTokenName(event.target.value)}
                placeholder="例如 Python SDK / 邮件机器人 / 自动化脚本"
                className="min-w-0 flex-1 bg-transparent text-base text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <button
              type="submit"
              disabled={creatingToken}
              className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {creatingToken ? <LoaderCircle className="animate-spin" size={18} /> : <Plus size={18} />}
              创建 TOKEN
            </button>
            <button
              type="button"
              onClick={() => setIsCreateFormOpen(false)}
              className="inline-flex items-center justify-center gap-2 rounded-2xl border border-white/20 bg-white/55 px-5 py-3 font-semibold text-gray-800 transition hover:bg-white/70 dark:border-white/10 dark:bg-black/30 dark:text-gray-100 dark:hover:bg-black/40"
            >
              取消
            </button>
          </div>
        </form>
      ) : null}

      {!isListExpanded ? (
        loading ? null : apiTokens.length === 0 ? (
          <div className="rounded-3xl border border-dashed border-white/20 bg-white/25 p-6 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/15 dark:text-gray-200">
            当前还没有 TOKEN。需要时再点击右上角“创建 TOKEN”即可。
          </div>
        ) : (
          <div className="space-y-3">
            <div className="text-sm font-medium text-gray-600 dark:text-gray-300">
              仅展示最近 {compactPreviewTokens.length} 个 TOKEN 摘要。
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              {compactPreviewTokens.map((item) => {
                const isRevoked = Boolean(item.revoked_at);
                return (
                  <div
                    key={item.public_id}
                    className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="truncate font-semibold text-gray-900 dark:text-white">{item.name}</div>
                        <div className="mt-1 truncate font-mono text-xs text-gray-500 dark:text-gray-400">{item.public_id}</div>
                      </div>
                      <StatusChip
                        label={isRevoked ? '已撤销' : '可用'}
                        className={
                          isRevoked
                            ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                            : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300'
                        }
                      />
                    </div>
                    <div className="mt-3 text-sm text-gray-600 dark:text-gray-300">
                      最近使用：{item.last_used_at ? formatDate(item.last_used_at) : '尚未使用'}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )
      ) : (
        <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
          <table className="w-full min-w-[760px] border-collapse text-left">
            <thead>
              <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                <th className="px-5 py-4 font-semibold">名称</th>
                <th className="px-5 py-4 font-semibold">公开 ID</th>
                <th className="px-5 py-4 font-semibold">最近使用</th>
                <th className="px-5 py-4 font-semibold">状态</th>
                <th className="px-5 py-4 font-semibold">操作</th>
              </tr>
            </thead>
            <tbody>
              {apiTokens.map((item) => {
                const isRevoked = Boolean(item.revoked_at);
                return (
                  <tr
                    key={item.public_id}
                    className="border-b border-white/10 last:border-b-0 hover:bg-white/30 dark:border-white/5 dark:hover:bg-white/5"
                  >
                    <td className="px-5 py-4 align-top">
                      <div className="font-semibold text-gray-900 dark:text-white">{item.name}</div>
                      <div className="mt-1 text-sm text-gray-500 dark:text-gray-400">创建于 {formatDate(item.created_at)}</div>
                    </td>
                    <td className="px-5 py-4 align-top font-mono text-sm text-gray-700 dark:text-gray-200">{item.public_id}</td>
                    <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{item.last_used_at ? formatDate(item.last_used_at) : '尚未使用'}</td>
                    <td className="px-5 py-4 align-top">
                      <StatusChip
                        label={isRevoked ? '已撤销' : '可用'}
                        className={
                          isRevoked
                            ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                            : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300'
                        }
                      />
                    </td>
                    <td className="px-5 py-4 align-top">
                      {!isRevoked ? (
                        <button
                          type="button"
                          onClick={() => void handleRevokeToken(item.public_id)}
                          disabled={Boolean(revokingTokenPublicIDs[item.public_id])}
                          className="inline-flex items-center gap-2 rounded-xl border border-red-200 bg-white/70 px-3 py-2 text-xs font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-800/35 dark:bg-black/20 dark:text-red-300 dark:hover:bg-red-950/20"
                        >
                          {revokingTokenPublicIDs[item.public_id] ? <LoaderCircle className="animate-spin" size={14} /> : <Trash2 size={14} />}
                          撤销 TOKEN
                        </button>
                      ) : (
                        <span className="text-sm text-gray-500 dark:text-gray-400">无可用操作</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </GlassCard>
  );
}

// InlineNotice 统一渲染组件内部的反馈消息条。
function InlineNotice({ tone, message }: SectionNotice) {
  const toneClassName =
    tone === 'success'
      ? 'border-emerald-300/40 bg-emerald-100/65 text-emerald-900 dark:border-emerald-700/35 dark:bg-emerald-950/30 dark:text-emerald-200'
      : tone === 'info'
        ? 'border-sky-300/40 bg-sky-100/65 text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/30 dark:text-sky-200'
        : 'border-red-300/40 bg-red-100/65 text-red-900 dark:border-red-700/35 dark:bg-red-950/30 dark:text-red-200';

  return <div className={`rounded-2xl border px-4 py-3 text-sm ${toneClassName}`}>{message}</div>;
}

// StatusChip 统一渲染 TOKEN 状态标签。
function StatusChip({ label, className }: { label: string; className: string }) {
  return <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${className}`}>{label}</span>;
}

// upsertAPIToken 把新增或更新的 TOKEN 合并回列表。
function upsertAPIToken(items: UserAPIToken[], nextItem: UserAPIToken): UserAPIToken[] {
  const nextItems = items.filter((item) => item.public_id !== nextItem.public_id);
  return [nextItem, ...nextItems].sort((left, right) => right.created_at.localeCompare(left.created_at));
}

// formatDate 把 ISO 时间转成人类可读时间。
function formatDate(value?: string): string {
  if (!value) {
    return '未记录';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(parsed);
}

// readableErrorMessage 把前端异常转成稳定的用户提示。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
