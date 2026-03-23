import { useEffect, useState, type FormEvent } from 'react';
import { motion } from 'motion/react';
import { GlassCard } from '../components/GlassCard';
import { Search, CheckCircle, XCircle, LoaderCircle, Sparkles, ArrowRight, Shuffle, CreditCard } from 'lucide-react';
import { APIError, checkAllocationAvailability, createAllocation, createMyDomainPurchaseOrder } from '../lib/api';
import { rememberLatestPaymentOrder } from '../lib/payment-tracking';
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

// DomainPurchaseMode 描述用户在搜索页中选择的购买模式。
type DomainPurchaseMode = 'exact' | 'random';

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

  // purchaseMode 记录当前购买面板使用的是精确前缀还是随机 12+ 字符模式。
  const [purchaseMode, setPurchaseMode] = useState<DomainPurchaseMode>('exact');

  // randomLength 保存随机字符购买时用户想要的长度，最低 12。
  const [randomLength, setRandomLength] = useState(12);

  // purchasePending 避免用户连续多次提交同一笔动态域名购买订单。
  const [purchasePending, setPurchasePending] = useState(false);

  // purchaseMessage 用于展示购买面板内的即时反馈。
  const [purchaseMessage, setPurchaseMessage] = useState('');

  // 当根域名列表首次加载完成时，优先选中默认根域名。
  useEffect(() => {
    if (selectedRootDomain || publicDomains.length === 0) {
      return;
    }

    const defaultDomain = publicDomains.find((item) => item.is_default) ?? publicDomains[0];
    setSelectedRootDomain(defaultDomain.root_domain);
  }, [publicDomains, selectedRootDomain]);

  // handleSearch 调用后端检查某个前缀是否可用。
  async function handleSearch(event: FormEvent): Promise<void> {
    event.preventDefault();
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

  // handlePurchase creates one dynamic LDC order for the current domain search context.
  async function handlePurchase(nextMode: DomainPurchaseMode): Promise<void> {
    if (!selectedDomainConfig?.sale_enabled) {
      setPurchaseMessage('当前后缀暂未开放购买。');
      return;
    }

    if (!authenticated) {
      onLogin();
      return;
    }

    if (!csrfToken) {
      setPurchaseMessage('当前会话缺少 CSRF Token，请刷新后重试。');
      return;
    }

    if (nextMode === 'exact') {
      if (!availability?.available) {
        setPurchaseMessage('请先查询一个当前可购买的精确前缀。');
        return;
      }

      const exactPriceCents = calculateExactPurchasePriceCents(selectedDomainConfig.sale_base_price_cents, availability.normalized_prefix.length);
      if (exactPriceCents === null) {
        setPurchaseMessage('1 字符前缀暂不出售，请更换更长的前缀。');
        return;
      }
    }

    if (nextMode === 'random' && (randomLength < 12 || randomLength > 63)) {
      setPurchaseMessage('随机字符模式仅支持 12 到 63 位长度。');
      return;
    }

    setPurchasePending(true);
    setPurchaseMessage('');

    try {
      const order = await createMyDomainPurchaseOrder(
        nextMode === 'exact'
          ? {
              root_domain: selectedDomainConfig.root_domain,
              mode: 'exact',
              prefix: availability?.normalized_prefix ?? domain,
            }
          : {
              root_domain: selectedDomainConfig.root_domain,
              mode: 'random',
              random_length: randomLength,
            },
        csrfToken,
      );

      rememberLatestPaymentOrder(order.out_trade_no);
      const openedWindow = window.open(order.payment_url, '_blank', 'noopener,noreferrer');
      if (!openedWindow) {
        window.location.assign(order.payment_url);
      }
      setPurchaseMessage(`订单 ${order.out_trade_no} 已创建，支付页已在新标签打开。支付完成后会自动为你发放新命名空间。`);
    } catch (error) {
      setPurchaseMessage(readableErrorMessage(error, '动态域名购买下单失败'));
    } finally {
      setPurchasePending(false);
    }
  }

  // selectedSuffix 用于展示当前输入框右侧动态变化的域名后缀。
  const selectedSuffix = selectedRootDomain || 'linuxdo.space';
  const selectedDomainConfig = publicDomains.find((item) => item.root_domain === selectedRootDomain) ?? null;
  const reservedPrefix = user?.username?.trim() || '你的用户名';
  const normalizedReservedPrefix = normalizeFrontendPrefix(user?.username ?? '');
  const canRegisterCurrentResult =
    availability?.available === true &&
    (!authenticated || availability.normalized_prefix === normalizedReservedPrefix);
  const exactPurchasePriceCents =
    selectedDomainConfig && availability?.available
      ? calculateExactPurchasePriceCents(selectedDomainConfig.sale_base_price_cents, availability.normalized_prefix.length)
      : null;
  const randomPurchasePriceCents =
    selectedDomainConfig && selectedDomainConfig.sale_enabled ? calculateRandomPurchasePriceCents(selectedDomainConfig.sale_base_price_cents) : null;

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
            搜索功能保持开放。当前免费自助仍只开放部分后缀下、与你的 Linux Do 用户名完全同名的子域名；额外命名空间购买也在本页完成。
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
                  setPurchaseMessage('');
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
                setDomain(event.target.value);
                setStatus('idle');
                setAvailability(null);
                setMessage('');
                setPurchaseMessage('');
              }}
              placeholder="输入你想要的域名前缀"
              className="w-full pl-4 pr-32 py-4 rounded-2xl bg-white/50 dark:bg-black/50 border border-white/40 dark:border-white/20 focus:outline-none focus:ring-2 focus:ring-teal-500 text-gray-900 dark:text-white placeholder-gray-500 dark:placeholder-gray-400 transition-all"
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
            查询
          </button>
        </form>

        {authenticated && (
          <div className="mt-4 text-sm text-gray-600 dark:text-gray-300">
            当前免费自助仍只开放开启免费流的同名子域。{selectedDomainConfig?.auto_provision ? (
              <>
                你可以直接申请 <span className="font-semibold text-teal-600 dark:text-teal-300">{reservedPrefix}.{selectedSuffix}</span>。
              </>
            ) : (
              '当前后缀不开放免费同名直领。'
            )}{' '}
            {selectedDomainConfig?.sale_enabled ? '下方已开放 LDC 购买更多命名空间。' : '当前后缀暂未开放付费购买。'}
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
                    disabled={status === 'creating' || !canRegisterCurrentResult}
                    className="px-6 py-2 rounded-xl bg-green-500 hover:bg-green-600 disabled:opacity-60 text-white font-medium transition-colors flex items-center gap-2"
                  >
                    {status === 'creating' ? <LoaderCircle size={16} className="animate-spin" /> : <Sparkles size={16} />}
                    {!authenticated ? '登录后注册' : canRegisterCurrentResult ? '立即注册' : '暂未开放注册'}
                  </button>
                ) : (
                  <span className="text-red-500 font-medium">当前不可申请</span>
                )}
              </div>
            )}

            {message && <div className="mt-4 text-sm text-gray-700 dark:text-gray-300">{message}</div>}
            {availability?.available && authenticated && !canRegisterCurrentResult && (
              <div className="mt-4 text-sm text-amber-700 dark:text-amber-300">
                该前缀当前无法走免费直领流程，但如果此后缀已开放购买，你可以在下方直接下单。
              </div>
            )}
          </motion.div>
        )}

        <div className="mt-6 rounded-2xl border border-white/20 bg-white/25 p-5 dark:bg-black/25">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2 text-lg font-semibold text-gray-900 dark:text-white">
                <CreditCard size={18} className="text-teal-500" />
                购买更多命名空间
              </div>
              <p className="mt-2 text-sm text-gray-600 dark:text-gray-300">
                管理员可为每个根域名单独定价。精确购买按前缀长度套用倍率，随机模式只卖 12 位及以上隐藏字符，付款成功后才会分配具体字符。
              </p>
            </div>
            <div className={`rounded-full px-4 py-2 text-sm font-semibold ${selectedDomainConfig?.sale_enabled ? 'bg-emerald-100/80 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-slate-100/80 text-slate-600 dark:bg-slate-900/40 dark:text-slate-300'}`}>
              {selectedDomainConfig?.sale_enabled ? `基础价 ${formatLDCAmount(selectedDomainConfig.sale_base_price_cents)} LDC / 个` : '当前后缀暂未开放购买'}
            </div>
          </div>

          <div className="mt-5 grid gap-3 rounded-2xl border border-white/20 bg-white/35 p-4 text-sm text-gray-700 dark:bg-black/30 dark:text-gray-300 md:grid-cols-4">
            <div>2 字符: 15 倍</div>
            <div>3 字符: 10 倍</div>
            <div>4 字符: 5 倍</div>
            <div>5 字符: 2 倍</div>
            <div>6 字符及以上: 1 倍</div>
            <div>1 字符: 不出售</div>
            <div className="md:col-span-2">随机 12 字符及以上: 0.5 倍，支付后才随机分配具体字符</div>
          </div>

          <div className="mt-5 flex flex-wrap gap-3">
            <button
              type="button"
              onClick={() => {
                setPurchaseMode('exact');
                setPurchaseMessage('');
              }}
              className={`rounded-full px-4 py-2 text-sm font-semibold transition-all ${purchaseMode === 'exact' ? 'bg-teal-500 text-white shadow-lg' : 'bg-white/55 text-gray-700 hover:bg-white/70 dark:bg-black/35 dark:text-gray-200 dark:hover:bg-black/50'}`}
            >
              精确购买
            </button>
            <button
              type="button"
              onClick={() => {
                setPurchaseMode('random');
                setPurchaseMessage('');
              }}
              className={`rounded-full px-4 py-2 text-sm font-semibold transition-all ${purchaseMode === 'random' ? 'bg-teal-500 text-white shadow-lg' : 'bg-white/55 text-gray-700 hover:bg-white/70 dark:bg-black/35 dark:text-gray-200 dark:hover:bg-black/50'}`}
            >
              随机字符购买
            </button>
          </div>

          {purchaseMode === 'exact' ? (
            <div className="mt-5 rounded-2xl border border-white/20 bg-white/35 p-4 dark:bg-black/30">
              <div className="flex flex-wrap items-center justify-between gap-4">
                <div>
                  <div className="text-sm text-gray-500 dark:text-gray-400">当前精确购买目标</div>
                  <div className="mt-1 font-mono text-base font-semibold text-gray-900 dark:text-white">
                    {availability?.fqdn ?? `${normalizeFrontendPrefix(domain) || 'your-prefix'}.${selectedSuffix}`}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-sm text-gray-500 dark:text-gray-400">预估价格</div>
                  <div className="mt-1 text-lg font-bold text-teal-600 dark:text-teal-300">
                    {exactPurchasePriceCents === null ? '暂不可购买' : `${formatLDCAmount(exactPurchasePriceCents)} LDC`}
                  </div>
                </div>
              </div>
              <div className="mt-3 text-sm text-gray-600 dark:text-gray-300">
                {!availability
                  ? '先查询精确前缀可用性，再在这里发起购买。'
                  : !availability.available
                  ? readableAvailabilityMessage(availability.reasons)
                  : exactPurchasePriceCents === null
                  ? '1 字符前缀不出售，请更换更长的前缀。'
                  : `当前长度 ${availability.normalized_prefix.length}，倍率 ${readableExactMultiplier(availability.normalized_prefix.length)}。`}
              </div>
              <button
                type="button"
                onClick={() => void handlePurchase('exact')}
                disabled={purchasePending || !selectedDomainConfig?.sale_enabled || !availability?.available || exactPurchasePriceCents === null}
                className="mt-4 inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-teal-500 to-emerald-500 px-5 py-3 font-semibold text-white shadow-lg transition-all hover:scale-[1.01] disabled:cursor-not-allowed disabled:opacity-60"
              >
                {purchasePending ? <LoaderCircle size={18} className="animate-spin" /> : <CreditCard size={18} />}
                {!authenticated ? '登录后购买' : '创建精确购买订单'}
              </button>
            </div>
          ) : (
            <div className="mt-5 rounded-2xl border border-white/20 bg-white/35 p-4 dark:bg-black/30">
              <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                <label className="block">
                  <div className="mb-2 text-sm text-gray-500 dark:text-gray-400">随机字符长度</div>
                  <input
                    type="number"
                    min={12}
                    max={63}
                    value={randomLength}
                    onChange={(event) => {
                      setRandomLength(Math.min(63, Math.max(12, Number(event.target.value) || 12)));
                      setPurchaseMessage('');
                    }}
                    className="w-full rounded-2xl border border-white/30 bg-white/70 px-4 py-3 text-gray-900 outline-none focus:ring-2 focus:ring-teal-500 dark:bg-black/35 dark:text-white"
                  />
                </label>
                <div className="rounded-2xl border border-white/20 bg-white/40 px-4 py-3 text-sm text-gray-700 dark:bg-black/25 dark:text-gray-200">
                  <div className="text-gray-500 dark:text-gray-400">随机模式价格</div>
                  <div className="mt-1 text-lg font-bold text-teal-600 dark:text-teal-300">
                    {randomPurchasePriceCents === null ? '暂未开放' : `${formatLDCAmount(randomPurchasePriceCents)} LDC`}
                  </div>
                </div>
              </div>
              <div className="mt-3 text-sm text-gray-600 dark:text-gray-300">
                随机模式不会在付款前展示具体字符，成功后才会把一个 {randomLength} 位随机前缀分配到你的账户名下。
              </div>
              <button
                type="button"
                onClick={() => void handlePurchase('random')}
                disabled={purchasePending || !selectedDomainConfig?.sale_enabled || randomPurchasePriceCents === null}
                className="mt-4 inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-cyan-500 to-teal-500 px-5 py-3 font-semibold text-white shadow-lg transition-all hover:scale-[1.01] disabled:cursor-not-allowed disabled:opacity-60"
              >
                {purchasePending ? <LoaderCircle size={18} className="animate-spin" /> : <Shuffle size={18} />}
                {!authenticated ? '登录后购买' : '创建随机购买订单'}
              </button>
            </div>
          )}

          {purchaseMessage ? (
            <div className="mt-4 rounded-2xl border border-white/20 bg-white/35 px-4 py-3 text-sm text-gray-700 dark:bg-black/30 dark:text-gray-200">
              {purchaseMessage}
            </div>
          ) : null}
        </div>
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
  if (reasons.includes('reserved_mail_namespace')) {
    return '所有以 -mail 结尾的前缀都被系统保留给邮箱命名空间，当前不可申请。';
  }
  if (reasons.includes('reserved_in_database')) {
    return '该前缀已经被平台分配给其他用户。';
  }
  if (reasons.includes('existing_dns_records')) {
    return 'Cloudflare 中已存在同命名空间记录，当前无法继续分配。';
  }
  return '该前缀当前不可申请。';
}

// calculateExactPurchasePriceCents mirrors the backend's fixed multiplier table.
function calculateExactPurchasePriceCents(basePriceCents: number, normalizedLength: number): number | null {
  if (basePriceCents <= 0) {
    return null;
  }
  if (normalizedLength <= 1) {
    return null;
  }
  if (normalizedLength === 2) {
    return basePriceCents * 15;
  }
  if (normalizedLength === 3) {
    return basePriceCents * 10;
  }
  if (normalizedLength === 4) {
    return basePriceCents * 5;
  }
  if (normalizedLength === 5) {
    return basePriceCents * 2;
  }
  return basePriceCents;
}

// calculateRandomPurchasePriceCents mirrors the backend's 0.5x random-mode rule.
function calculateRandomPurchasePriceCents(basePriceCents: number): number | null {
  if (basePriceCents <= 0) {
    return null;
  }
  return Math.ceil(basePriceCents / 2);
}

// readableExactMultiplier converts the fixed pricing table into UI copy.
function readableExactMultiplier(normalizedLength: number): string {
  if (normalizedLength <= 1) {
    return '不出售';
  }
  if (normalizedLength === 2) {
    return '15 倍';
  }
  if (normalizedLength === 3) {
    return '10 倍';
  }
  if (normalizedLength === 4) {
    return '5 倍';
  }
  if (normalizedLength === 5) {
    return '2 倍';
  }
  return '1 倍';
}

// formatLDCAmount converts cents into the Linux Do Credit decimal display string.
function formatLDCAmount(cents: number): string {
  const whole = Math.floor(cents / 100);
  const fraction = cents % 100;
  if (fraction === 0) {
    return String(whole);
  }
  return `${whole}.${String(fraction).padStart(2, '0')}`;
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

// normalizeFrontendPrefix 复用后端同样的清洗思路，避免前端按钮状态与后端限制出现明显偏差。
function normalizeFrontendPrefix(raw: string): string {
  const value = raw.trim().toLowerCase();
  if (value === '') {
    return '';
  }

  let normalized = '';
  let lastWasDash = false;
  for (const char of value) {
    const isLower = char >= 'a' && char <= 'z';
    const isDigit = char >= '0' && char <= '9';

    if (isLower || isDigit) {
      normalized += char;
      lastWasDash = false;
      continue;
    }

    if (!lastWasDash) {
      normalized += '-';
      lastWasDash = true;
    }
  }

  normalized = normalized.replace(/^-+|-+$/g, '');
  if (normalized.length > 63) {
    normalized = normalized.slice(0, 63).replace(/-+$/g, '');
  }
  return normalized;
}
