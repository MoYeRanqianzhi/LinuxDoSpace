import { useEffect, useState, type FormEvent } from 'react';
import { motion } from 'motion/react';
import { GlassCard } from '../components/GlassCard';
import { Search, CheckCircle, XCircle, LoaderCircle, Sparkles, ArrowRight } from 'lucide-react';
import { APIError, checkAllocationAvailability, createAllocation } from '../lib/api';
import type { Allocation, AvailabilityResult, ManagedDomain, User } from '../types/api';

// DomainsProps 描述域名分发页所需的后端状态与交互函数。
interface DomainsProps {
  publicDomains: ManagedDomain[];
  domainsLoading: boolean;
  domainsError: string;
  authenticated: boolean;
  user?: User;
  allocations: Allocation[];
  csrfToken?: string;
  onLogin: () => void;
  onAllocationCreated: () => Promise<void>;
}

// SearchStatus 用于描述域名查询和申请流程当前处于哪个阶段。
type SearchStatus = 'idle' | 'checking' | 'available' | 'taken' | 'creating' | 'error';

// Domains 负责承接“查询前缀是否可用”和“为当前用户申请命名空间”两项核心操作。
export function Domains({
  publicDomains,
  domainsLoading,
  domainsError,
  authenticated,
  user,
  allocations,
  csrfToken,
  onLogin,
  onAllocationCreated,
}: DomainsProps) {
  // domain 保存用户输入的原始前缀文本。
  const [domain, setDomain] = useState('');

  // selectedRootDomain 保存当前用户正在查询的根域名。
  const [selectedRootDomain, setSelectedRootDomain] = useState('');

  // status 保存查询与创建流程的当前状态。
  const [status, setStatus] = useState<SearchStatus>('idle');

  // availability 保存最近一次查询返回的结果。
  const [availability, setAvailability] = useState<AvailabilityResult | null>(null);

  // message 用于在界面中展示成功或失败原因。
  const [message, setMessage] = useState('');

  // 当根域名列表首次加载完成时，优先选中默认根域名。
  useEffect(() => {
    if (selectedRootDomain || publicDomains.length === 0) {
      return;
    }

    const defaultDomain = publicDomains.find((item) => item.is_default) ?? publicDomains[0];
    setSelectedRootDomain(defaultDomain.root_domain);
  }, [publicDomains, selectedRootDomain]);

  // 当前临时阶段只允许申请与 Linux Do 用户名同名的子域，因此登录后自动锁定输入框。
  useEffect(() => {
    if (!authenticated || !user?.username) {
      return;
    }

    setDomain(user.username);
    setStatus('idle');
    setAvailability(null);
    setMessage('');
  }, [authenticated, user?.username]);

  // handleSearch 调用后端检查某个前缀是否可用。
  async function handleSearch(event: FormEvent): Promise<void> {
    event.preventDefault();
    if (!authenticated) {
      onLogin();
      return;
    }
    if (!domain.trim() || !selectedRootDomain) {
      return;
    }

    setStatus('checking');
    setMessage('');

    try {
      const result = await checkAllocationAvailability(selectedRootDomain, domain);
      setAvailability(result);
      setStatus(result.available ? 'available' : 'taken');

      if (result.available) {
        setMessage('当前前缀可用，你可以立即申请。');
      } else {
        setMessage(readableAvailabilityMessage(result.reasons));
      }
    } catch (error) {
      setAvailability(null);
      setStatus('error');
      setMessage(readableErrorMessage(error, '域名可用性检查失败'));
    }
  }

  // handleRegister 为当前登录用户申请命名空间。
  async function handleRegister(): Promise<void> {
    if (!availability?.available) {
      return;
    }

    if (!authenticated) {
      onLogin();
      return;
    }

    if (!csrfToken) {
      setStatus('error');
      setMessage('当前会话缺少 CSRF Token，请刷新后重试。');
      return;
    }

    setStatus('creating');
    setMessage('');

    try {
      await createAllocation(
        {
          root_domain: availability.root_domain,
          prefix: domain,
          source: 'manual',
          primary: allocations.length === 0,
        },
        csrfToken,
      );

      setMessage('命名空间申请成功，正在跳转到配置中心。');
      await onAllocationCreated();
    } catch (error) {
      setStatus('error');
      setMessage(readableErrorMessage(error, '命名空间申请失败'));
    }
  }

  // selectedSuffix 用于展示当前输入框右侧动态变化的域名后缀。
  const selectedSuffix = selectedRootDomain || 'linuxdo.space';
  const reservedPrefix = user?.username?.trim() || '你的用户名';

  return (
    <div className="max-w-4xl mx-auto pt-32 pb-24 px-6">
      <motion.div
        initial={{ y: 20, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        className="text-center mb-12"
      >
        <h1 className="text-4xl md:text-5xl font-extrabold mb-4 text-gray-900 dark:text-white">
          寻找你的专属域名
        </h1>
        <p className="text-lg text-gray-700 dark:text-gray-300">
          目前支持{' '}
          <span className="font-bold text-teal-500">
            {selectedRootDomain || (publicDomains[0]?.root_domain ?? 'linuxdo.space')}
          </span>{' '}
          等后缀
        </p>
      </motion.div>

      <GlassCard className="mb-8">
        {!authenticated && (
          <div className="mb-5 rounded-2xl border border-amber-300/40 bg-amber-100/60 dark:bg-amber-950/25 dark:border-amber-700/40 px-4 py-4 text-sm text-amber-900 dark:text-amber-200">
            当前临时阶段仅允许领取与你的 Linux Do 用户名完全同名的子域名。请先登录后继续。
          </div>
        )}

        <div className="mb-5 flex flex-wrap gap-3">
          {domainsLoading ? (
            <div className="text-sm text-gray-600 dark:text-gray-300">正在加载可分发域名列表...</div>
          ) : publicDomains.length > 0 ? (
            publicDomains.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => {
                  setSelectedRootDomain(item.root_domain);
                  setStatus('idle');
                  setAvailability(null);
                  setMessage('');
                }}
                className={`rounded-full px-4 py-2 text-sm font-semibold transition-all ${
                  selectedRootDomain === item.root_domain
                    ? 'bg-teal-500 text-white shadow-lg'
                    : 'bg-white/45 dark:bg-black/30 text-gray-700 dark:text-gray-200 hover:bg-white/65 dark:hover:bg-black/50'
                }`}
              >
                {item.root_domain}
              </button>
            ))
          ) : (
            <div className="text-sm text-red-600 dark:text-red-300">
              {domainsError || '当前没有可分发的根域名。'}
            </div>
          )}
        </div>

        <form onSubmit={handleSearch} className="flex flex-col sm:flex-row gap-4">
          <div className="relative flex-1">
            <input
              type="text"
              value={domain}
              onChange={(event) => {
                if (authenticated) {
                  return;
                }
                setDomain(event.target.value);
                setStatus('idle');
                setAvailability(null);
                setMessage('');
              }}
              readOnly={authenticated}
              placeholder={authenticated ? reservedPrefix : '输入你想要的域名前缀'}
              className="w-full pl-4 pr-32 py-4 rounded-2xl bg-white/50 dark:bg-black/50 border border-white/40 dark:border-white/20 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white placeholder-gray-500 dark:placeholder-gray-400 transition-all read-only:cursor-not-allowed read-only:opacity-80"
            />
            <div className="absolute right-4 top-1/2 -translate-y-1/2 text-gray-500 font-medium">
              .{selectedSuffix}
            </div>
          </div>
          <button
            type="submit"
            disabled={domainsLoading || !selectedRootDomain || status === 'checking'}
            className="flex items-center justify-center gap-2 px-8 py-4 rounded-2xl bg-gradient-to-r from-teal-500 to-emerald-500 hover:from-teal-600 hover:to-emerald-600 disabled:opacity-60 disabled:cursor-not-allowed text-white font-bold shadow-lg transition-all transform hover:scale-105"
          >
            {status === 'checking' ? <LoaderCircle size={20} className="animate-spin" /> : <Search size={20} />}
            {authenticated ? '检查我的同名子域' : '登录后继续'}
          </button>
        </form>

        {authenticated && (
          <div className="mt-4 text-sm text-gray-600 dark:text-gray-300">
            当前只允许申请 <span className="font-semibold text-teal-600 dark:text-teal-300">{reservedPrefix}.{selectedSuffix}</span>
          </div>
        )}

        {(status !== 'idle' || message) && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            className="mt-6 p-4 rounded-2xl bg-white/30 dark:bg-black/30 border border-white/20"
          >
            {availability && (
              <div className="flex items-center justify-between gap-4 flex-wrap">
                <div className="flex items-center gap-3">
                  {status === 'available' || status === 'creating' ? (
                    <CheckCircle className="text-green-500 w-6 h-6" />
                  ) : (
                    <XCircle className="text-red-500 w-6 h-6" />
                  )}
                  <span className="text-lg font-medium text-gray-900 dark:text-white">
                    {availability.fqdn}
                  </span>
                </div>

                {status === 'available' || status === 'creating' ? (
                  <button
                    type="button"
                    onClick={() => void handleRegister()}
                    disabled={status === 'creating'}
                    className="px-6 py-2 rounded-xl bg-green-500 hover:bg-green-600 disabled:opacity-60 text-white font-medium transition-colors flex items-center gap-2"
                  >
                    {status === 'creating' ? <LoaderCircle size={16} className="animate-spin" /> : <Sparkles size={16} />}
                    {authenticated ? '立即注册' : '登录后注册'}
                  </button>
                ) : (
                  <span className="text-red-500 font-medium">当前不可申请</span>
                )}
              </div>
            )}

            {message && <div className="mt-4 text-sm text-gray-700 dark:text-gray-300">{message}</div>}
          </motion.div>
        )}
      </GlassCard>

      {!authenticated && (
        <div className="mb-8 flex justify-center">
          <button
            type="button"
            onClick={onLogin}
            className="inline-flex items-center gap-2 px-6 py-3 rounded-2xl bg-[#1a1a1a] dark:bg-white hover:bg-black dark:hover:bg-gray-100 text-white dark:text-black font-bold shadow-lg transition-all"
          >
            <ArrowRight size={18} />
            使用 Linux Do 登录
          </button>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {[1, 2, 3].map((item) => (
          <div key={item}>
            <GlassCard delay={0.2 + item * 0.1} className="text-center">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-gradient-to-br from-teal-400 to-emerald-400 flex items-center justify-center text-white font-bold text-xl shadow-lg">
                {item}
              </div>
              <h3 className="text-lg font-bold mb-2 text-gray-900 dark:text-white">
                {item === 1 ? '选择前缀' : item === 2 ? '验证身份' : '配置解析'}
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                {item === 1
                  ? '输入心仪的域名前缀并调用后端真实检查接口'
                  : item === 2
                  ? '通过 Linux Do 账号授权登录后申请命名空间'
                  : '在配置中心直接管理 Cloudflare DNS 记录'}
              </p>
            </GlassCard>
          </div>
        ))}
      </div>
    </div>
  );
}

// readableAvailabilityMessage 把后端返回的占用原因翻译成更直观的文案。
function readableAvailabilityMessage(reasons: string[]): string {
  if (reasons.includes('reserved_in_database')) {
    return '该前缀已经被平台分配给其他用户。';
  }
  if (reasons.includes('existing_dns_records')) {
    return 'Cloudflare 中已存在同命名空间记录，当前无法继续分配。';
  }
  return '该前缀当前不可申请。';
}

// readableErrorMessage 用于统一提取接口错误文本。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
