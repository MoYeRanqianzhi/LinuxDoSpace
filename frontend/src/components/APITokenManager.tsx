import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Copy, KeyRound, LoaderCircle, Plus, RefreshCw } from 'lucide-react';
import { GlassCard } from './GlassCard';
import { APIError, createMyAPIToken, listMyAPITokens, revokeMyAPIToken } from '../lib/api';
import type { UserAPIToken } from '../types/api';

// APITokenManagerProps 描述通用 TOKEN 管理卡片所需的最小输入。
// 该组件刻意不依赖 DNS 或邮箱页面状态，避免再次被放错位置。
interface APITokenManagerProps {
  csrfToken?: string;
}

// NoticeTone 统一描述卡片中的反馈语气。
type NoticeTone = 'error' | 'success' | 'info';

// SectionNotice 用于在组件内展示用户可读的状态信息。
interface SectionNotice {
  tone: NoticeTone;
  message: string;
}

// APITokenManager 负责创建、展示与撤销用户的通用 API TOKEN。
// 这些 TOKEN 会在邮箱页中作为可选实时收件目标，也会给后续 API 能力复用。
export function APITokenManager({ csrfToken }: APITokenManagerProps) {
  // apiTokens 保存当前用户已创建的全部 TOKEN。
  const [apiTokens, setApiTokens] = useState<UserAPIToken[]>([]);

  // loading 控制首次加载和刷新时的骨架状态。
  const [loading, setLoading] = useState(true);

  // tokenError 用于显示列表读取失败。
  const [tokenError, setTokenError] = useState('');

  // tokenNotice 用于显示创建、复制、撤销等操作反馈。
  const [tokenNotice, setTokenNotice] = useState<SectionNotice | null>(null);

  // newTokenName 保存“创建 TOKEN”输入框中的名称。
  const [newTokenName, setNewTokenName] = useState('');

  // creatingToken 控制创建按钮的提交中状态。
  const [creatingToken, setCreatingToken] = useState(false);

  // createdTokenSecret 保存新建 TOKEN 后后端一次性返回的原始密钥。
  const [createdTokenSecret, setCreatedTokenSecret] = useState('');

  // revokingTokenPublicIDs 用于逐行标记正在撤销中的 TOKEN。
  const [revokingTokenPublicIDs, setRevokingTokenPublicIDs] = useState<Record<string, boolean>>({});

  // activeAPITokens 统计当前仍可用的 EMAIL TOKEN 数量。
  const activeAPITokens = useMemo(
    () => apiTokens.filter((item) => item.email_enabled && !item.revoked_at),
    [apiTokens],
  );

  // 首次挂载时加载 TOKEN 列表。
  useEffect(() => {
    void loadTokens();
  }, []);

  // loadTokens 从后端读取当前用户已创建的 TOKEN 列表。
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

  // handleCreateToken 创建一个支持 EMAIL 能力的新 TOKEN。
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

  // handleRevokeToken 撤销指定 TOKEN，阻止后续新的实时连接继续使用它。
  async function handleRevokeToken(publicID: string): Promise<void> {
    if (!csrfToken) {
      setTokenNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    try {
      setRevokingTokenPublicIDs((current) => ({ ...current, [publicID]: true }));
      const item = await revokeMyAPIToken(publicID, csrfToken);
      setApiTokens((currentItems) => upsertAPIToken(currentItems, item));
      setTokenNotice({ tone: 'info', message: `TOKEN ${item.name} 已撤销，新的实时连接将不再被接受。` });
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
    <GlassCard className="space-y-5">
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-2xl bg-violet-500/15 p-3 text-violet-700 dark:text-violet-300">
            <KeyRound size={20} />
          </div>
          <div>
            <h2 className="text-xl font-bold text-gray-900 dark:text-white">通用 API TOKEN</h2>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
              在配置中心统一创建和管理 TOKEN。邮箱页会把这些 TOKEN 作为可选实时收件目标，后续 API 能力也会复用这里的凭据。
            </p>
          </div>
        </div>

        <button
          type="button"
          onClick={() => void loadTokens()}
          disabled={loading}
          className="inline-flex items-center justify-center gap-2 rounded-2xl border border-white/20 bg-white/55 px-4 py-3 text-sm font-semibold text-gray-800 transition hover:bg-white/70 disabled:cursor-not-allowed disabled:opacity-60 dark:border-white/10 dark:bg-black/30 dark:text-gray-100 dark:hover:bg-black/40"
        >
          {loading ? <LoaderCircle className="animate-spin" size={16} /> : <RefreshCw size={16} />}
          刷新 TOKEN
        </button>
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

      <div className="grid gap-3 md:grid-cols-3">
        <InfoStat title="可用 TOKEN" value={`${activeAPITokens.length} 个`} />
        <InfoStat title="全部 TOKEN" value={`${apiTokens.length} 个`} />
        <InfoStat title="能力" value="EMAIL 实时流" />
      </div>

      <div className="rounded-2xl border border-white/15 bg-white/35 p-4 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/20 dark:text-gray-200">
        TOKEN 被设置为邮箱目标后，只有在客户端保持连接时才会收到实时邮件事件；如果没有连接，服务器会直接丢弃该目标邮件，不会为了 TOKEN 目标额外堆积队列。
      </div>

      <form className="space-y-4" onSubmit={(event) => void handleCreateToken(event)}>
        <div className="grid gap-3 lg:grid-cols-[1fr_auto]">
          <div className="flex min-w-0 items-center rounded-2xl border border-white/20 bg-white/55 px-4 py-3 shadow-inner dark:border-white/10 dark:bg-black/35">
            <input
              type="text"
              value={newTokenName}
              onChange={(event) => setNewTokenName(event.target.value)}
              placeholder="例如 Python SDK / 邮件机器人 / 自建客户端"
              className="min-w-0 flex-1 bg-transparent text-base text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500"
            />
          </div>
          <button
            type="submit"
            disabled={creatingToken}
            className="inline-flex items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-violet-500 to-fuchsia-600 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-violet-600 hover:to-fuchsia-700 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {creatingToken ? <LoaderCircle className="animate-spin" size={18} /> : <Plus size={18} />}
            创建 TOKEN
          </button>
        </div>
      </form>

      {loading ? (
        <div className="rounded-3xl border border-dashed border-white/20 bg-white/25 p-6 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/15 dark:text-gray-200">
          正在加载你的 API TOKEN 列表...
        </div>
      ) : apiTokens.length === 0 ? (
        <div className="rounded-3xl border border-dashed border-white/20 bg-white/25 p-6 text-sm leading-7 text-gray-700 dark:border-white/10 dark:bg-black/15 dark:text-gray-200">
          你当前还没有创建任何 API TOKEN。创建后，它们会在邮箱页中出现在目标下拉框里，可直接作为实时收件目标使用。
        </div>
      ) : (
        <div className="overflow-x-auto rounded-3xl border border-white/15 bg-white/35 dark:border-white/10 dark:bg-black/20">
          <table className="w-full min-w-[820px] border-collapse text-left">
            <thead>
              <tr className="border-b border-white/15 text-sm text-gray-600 dark:border-white/10 dark:text-gray-300">
                <th className="px-5 py-4 font-semibold">名称</th>
                <th className="px-5 py-4 font-semibold">公开 ID</th>
                <th className="px-5 py-4 font-semibold">能力</th>
                <th className="px-5 py-4 font-semibold">最近使用</th>
                <th className="px-5 py-4 font-semibold">状态</th>
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
                    <td className="px-5 py-4 align-top text-sm font-mono text-gray-700 dark:text-gray-200">{item.public_id}</td>
                    <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{item.email_enabled ? 'EMAIL 实时流' : '未启用'}</td>
                    <td className="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-200">{item.last_used_at ? formatDate(item.last_used_at) : '尚未使用'}</td>
                    <td className="px-5 py-4 align-top">
                      <div className="flex flex-col items-start gap-3">
                        <StatusChip
                          label={isRevoked ? '已撤销' : '可用'}
                          className={
                            isRevoked
                              ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                              : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/25 dark:text-emerald-300'
                          }
                        />
                        {!isRevoked ? (
                          <button
                            type="button"
                            onClick={() => void handleRevokeToken(item.public_id)}
                            disabled={Boolean(revokingTokenPublicIDs[item.public_id])}
                            className="inline-flex items-center gap-2 rounded-xl border border-red-200 bg-white/70 px-3 py-2 text-xs font-semibold text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-red-800/35 dark:bg-black/20 dark:text-red-300 dark:hover:bg-red-950/20"
                          >
                            {revokingTokenPublicIDs[item.public_id] ? <LoaderCircle className="animate-spin" size={14} /> : <RefreshCw size={14} />}
                            撤销 TOKEN
                          </button>
                        ) : null}
                      </div>
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

// InfoStat 渲染卡片中的小型统计块。
function InfoStat({ title, value }: { title: string; value: string }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
      <div className="text-xs font-semibold uppercase tracking-[0.22em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className="mt-2 text-lg font-bold text-gray-900 dark:text-white">{value}</div>
    </div>
  );
}

// InlineNotice 统一渲染组件内部的操作反馈。
function InlineNotice({ tone, message }: SectionNotice) {
  const toneClassName =
    tone === 'success'
      ? 'border-emerald-300/40 bg-emerald-100/65 text-emerald-900 dark:border-emerald-700/35 dark:bg-emerald-950/30 dark:text-emerald-200'
      : tone === 'info'
        ? 'border-sky-300/40 bg-sky-100/65 text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/30 dark:text-sky-200'
        : 'border-red-300/40 bg-red-100/65 text-red-900 dark:border-red-700/35 dark:bg-red-950/30 dark:text-red-200';

  return <div className={`rounded-2xl border px-4 py-3 text-sm ${toneClassName}`}>{message}</div>;
}

// StatusChip 用于在 TOKEN 列表中显示简洁状态标签。
function StatusChip({ label, className }: { label: string; className: string }) {
  return <span className={`inline-flex rounded-full px-3 py-1 text-xs font-semibold ${className}`}>{label}</span>;
}

// upsertAPIToken 把新增或更新的 TOKEN 合并回当前列表。
function upsertAPIToken(items: UserAPIToken[], nextItem: UserAPIToken): UserAPIToken[] {
  const nextItems = items.filter((item) => item.public_id !== nextItem.public_id);
  return [nextItem, ...nextItems].sort((left, right) => right.created_at.localeCompare(left.created_at));
}

// formatDate 统一把 ISO 时间转成人类可读格式。
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

// readableErrorMessage 把前端异常转换成稳定的可读错误提示。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
