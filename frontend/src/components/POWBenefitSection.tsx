import { useEffect, useMemo, useRef, useState } from 'react';
import { Cpu, Gift, LoaderCircle, Pickaxe, RefreshCw, ShieldAlert, Sparkles, StopCircle } from 'lucide-react';

import { claimMyPOWChallenge, createMyPOWChallenge, getMyPOWStatus, APIError } from '../lib/api';
import type { POWChallenge, POWStatus, UserPermission } from '../types/api';
import { GlassCard } from './GlassCard';
import { GlassSelect, type GlassSelectOption } from './GlassSelect';

interface POWBenefitSectionProps {
  authenticated: boolean;
  csrfToken?: string;
  catchAllPermission?: UserPermission | null;
  onLogin: () => void;
  onRewardClaimed?: () => Promise<void> | void;
}

type NoticeTone = 'error' | 'success' | 'info';

interface SectionNotice {
  tone: NoticeTone;
  message: string;
}

interface SolveProgress {
  attempts: number;
  elapsedMs: number;
  bestLeadingZeroBits: number;
}

interface SolverProgressMessage {
  type: 'progress';
  job_id: string;
  attempts: number;
  elapsed_ms: number;
  best_leading_zero_bits: number;
}

interface SolverSolvedMessage {
  type: 'solved';
  job_id: string;
  nonce: string;
  attempts: number;
  elapsed_ms: number;
  leading_zero_bits: number;
}

interface SolverStoppedMessage {
  type: 'stopped';
  job_id: string;
  attempts: number;
  elapsed_ms: number;
}

interface SolverErrorMessage {
  type: 'error';
  job_id: string;
  message: string;
}

type SolverMessage = SolverProgressMessage | SolverSolvedMessage | SolverStoppedMessage | SolverErrorMessage;

const fallbackBenefitKey = 'email_catch_all_remaining_count';
const fallbackDifficulty = '3';

// POWBenefitSection renders the browser-side PoW welfare panel that sits below
// the LDC exchange area. The backend still owns challenge generation and
// verification; the frontend only uses a worker to search for a valid nonce.
export function POWBenefitSection({
  authenticated,
  csrfToken,
  catchAllPermission,
  onLogin,
  onRewardClaimed,
}: POWBenefitSectionProps) {
  const [status, setStatus] = useState<POWStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [claiming, setClaiming] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState<SectionNotice | null>(null);
  const [selectedBenefitKey, setSelectedBenefitKey] = useState(fallbackBenefitKey);
  const [selectedDifficulty, setSelectedDifficulty] = useState(fallbackDifficulty);
  const [solving, setSolving] = useState(false);
  const [solveProgress, setSolveProgress] = useState<SolveProgress | null>(null);

  const workerRef = useRef<Worker | null>(null);
  const activeJobIDRef = useRef('');
  const lastChallengeIDRef = useRef<number | null>(null);

  const benefitOptions = useMemo<GlassSelectOption[]>(
    () => (status?.benefits ?? []).filter((item) => item.enabled).map((item) => ({
      value: item.key,
      label: item.display_name,
    })),
    [status?.benefits],
  );
  const difficultyOptions = useMemo<GlassSelectOption[]>(
    () => (status?.difficulty_options ?? []).map((item) => ({
      value: String(item.value),
      label: `${item.label} · ${item.reward_multiplier}x 奖励`,
    })),
    [status?.difficulty_options],
  );
  const currentChallenge = status?.current_challenge;
  const selectedDifficultyValue = Number.parseInt(selectedDifficulty, 10) || 3;
  const selectedDifficultyMeta = useMemo(
    () => status?.difficulty_options.find((item) => item.value === selectedDifficultyValue) ?? null,
    [selectedDifficultyValue, status?.difficulty_options],
  );
  const canGenerate = authenticated && Boolean(csrfToken) && !creating && !claiming && (status?.remaining_today ?? 0) > 0;
  const canSolve = canGenerate && Boolean(currentChallenge) && !solving;

  useEffect(() => {
    if (!authenticated) {
      stopWorker();
      setStatus(null);
      setLoading(false);
      setCreating(false);
      setClaiming(false);
      setError('');
      setNotice(null);
      setSolving(false);
      setSolveProgress(null);
      setSelectedBenefitKey(fallbackBenefitKey);
      setSelectedDifficulty(fallbackDifficulty);
      return;
    }

    void loadStatus();
  }, [authenticated]);

  useEffect(() => {
    const firstBenefit = benefitOptions[0]?.value ?? fallbackBenefitKey;
    if (!benefitOptions.some((item) => item.value === selectedBenefitKey)) {
      setSelectedBenefitKey(firstBenefit);
    }
  }, [benefitOptions, selectedBenefitKey]);

  useEffect(() => {
    const firstDifficulty = difficultyOptions[0]?.value ?? fallbackDifficulty;
    if (!difficultyOptions.some((item) => item.value === selectedDifficulty)) {
      setSelectedDifficulty(firstDifficulty);
    }
  }, [difficultyOptions, selectedDifficulty]);

  useEffect(() => {
    if (!currentChallenge) {
      stopWorker();
      setSolving(false);
      setSolveProgress(null);
      lastChallengeIDRef.current = null;
      return;
    }

    if (lastChallengeIDRef.current !== null && lastChallengeIDRef.current !== currentChallenge.id) {
      stopWorker();
      setSolving(false);
      setSolveProgress(null);
    }
    lastChallengeIDRef.current = currentChallenge.id;
  }, [currentChallenge?.id]);

  useEffect(() => () => stopWorker(), []);

  async function loadStatus(): Promise<void> {
    try {
      setLoading(true);
      const nextStatus = await getMyPOWStatus();
      setStatus(nextStatus);
      setError('');
    } catch (loadError) {
      setStatus(null);
      setError(readableErrorMessage(loadError, '无法加载 PoW 福利状态。'));
    } finally {
      setLoading(false);
    }
  }

  async function handleCreateChallenge(): Promise<void> {
    if (!authenticated) {
      onLogin();
      return;
    }
    if (!csrfToken) {
      setNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }
    if (!canGenerate) {
      return;
    }

    try {
      setCreating(true);
      setNotice(null);
      stopWorker();
      const challenge = await createMyPOWChallenge({
        benefit_key: selectedBenefitKey,
        difficulty: selectedDifficultyValue,
      }, csrfToken);
      setStatus((currentStatus) => {
        if (!currentStatus) return null;
        return {
          ...currentStatus,
          current_challenge: challenge,
        };
      });
      setSolveProgress(null);
      setNotice({
        tone: 'success',
        message: `新的 PoW 题目已生成，本题奖励 ${challenge.reward_quantity}${challenge.reward_unit}。你现在可以启动浏览器 worker 开始解题。`,
      });
    } catch (createError) {
      setNotice({ tone: 'error', message: readableErrorMessage(createError, '生成 PoW 题目失败。') });
    } finally {
      setCreating(false);
    }
  }

  function handleStartSolving(): void {
    if (!currentChallenge || !canSolve) {
      return;
    }

    stopWorker();
    const worker = new Worker(new URL('../workers/pow-solver.worker.ts', import.meta.url), { type: 'module' });
    const jobID = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `${currentChallenge.id}-${Date.now()}`;

    workerRef.current = worker;
    activeJobIDRef.current = jobID;
    setNotice({
      tone: 'info',
      message: `浏览器 worker 已启动，正在为题目 ${currentChallenge.id} 搜索有效 nonce。当前难度按前导零 bit 数计算。`,
    });
    setSolving(true);
    setSolveProgress({
      attempts: 0,
      elapsedMs: 0,
      bestLeadingZeroBits: 0,
    });

    worker.onmessage = (event: MessageEvent<SolverMessage>) => {
      const message = event.data;
      if (message.job_id !== activeJobIDRef.current) {
        return;
      }

      switch (message.type) {
        case 'progress':
          setSolveProgress({
            attempts: message.attempts,
            elapsedMs: message.elapsed_ms,
            bestLeadingZeroBits: message.best_leading_zero_bits,
          });
          break;
        case 'solved':
          setSolveProgress({
            attempts: message.attempts,
            elapsedMs: message.elapsed_ms,
            bestLeadingZeroBits: message.leading_zero_bits,
          });
          setNotice({
            tone: 'info',
            message: `已在浏览器本地找到有效 nonce，正在提交后端验题并发放奖励。总尝试次数 ${message.attempts.toLocaleString('zh-CN')}。`,
          });
          stopWorker(false);
          void handleClaimChallenge(currentChallenge, message.nonce);
          break;
        case 'stopped':
          stopWorker(false);
          setNotice({
            tone: 'info',
            message: `解题已停止，本次共尝试 ${message.attempts.toLocaleString('zh-CN')} 次。`,
          });
          break;
        case 'error':
          stopWorker(false);
          setNotice({ tone: 'error', message: `解题 worker 出错：${message.message}` });
          break;
      }
    };

    worker.postMessage({
      type: 'start',
      job_id: jobID,
      challenge: currentChallenge,
    });
  }

  function handleStopSolving(): void {
    stopWorker();
    setNotice({ tone: 'info', message: '浏览器 worker 已手动停止，当前题目仍然保留，你可以稍后继续。' });
  }

  async function handleClaimChallenge(challenge: POWChallenge, nonce: string): Promise<void> {
    if (!csrfToken) {
      setNotice({ tone: 'error', message: '当前会话缺少 CSRF Token，请重新登录后再试。' });
      return;
    }

    try {
      setClaiming(true);
      const result = await claimMyPOWChallenge({
        challenge_id: challenge.id,
        nonce,
      }, csrfToken);

      await loadStatus();
      if (onRewardClaimed) {
        await onRewardClaimed();
      }

      setNotice({
        tone: 'success',
        message: `PoW 奖励已到账，本次发放 ${result.granted_quantity}${result.reward_unit}。当前邮箱泛解析剩余次数为 ${result.current_remaining_count.toLocaleString('zh-CN')}。`,
      });
    } catch (claimError) {
      setNotice({ tone: 'error', message: readableErrorMessage(claimError, 'PoW 奖励领取失败。') });
      await loadStatus();
    } finally {
      setClaiming(false);
      setSolving(false);
    }
  }

  function stopWorker(resetState = true): void {
    if (workerRef.current) {
      if (activeJobIDRef.current) {
        workerRef.current.postMessage({ type: 'stop', job_id: activeJobIDRef.current });
      }
      workerRef.current.terminate();
      workerRef.current = null;
    }
    activeJobIDRef.current = '';
    if (resetState) {
      setSolving(false);
    }
  }

  const permissionStatusLabel = describePermissionStatus(catchAllPermission?.status ?? 'not_requested');

  return (
    <GlassCard className="mt-8 space-y-6">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <div className="inline-flex items-center gap-2 rounded-full bg-amber-100/80 px-3 py-1 text-xs font-semibold text-amber-900 dark:bg-amber-900/25 dark:text-amber-100">
            <Pickaxe size={14} />
            PoW 福利
          </div>
          <h2 className="mt-3 text-2xl font-bold text-gray-900 dark:text-white">解开独立谜题，换取邮箱泛解析福利</h2>
          <p className="mt-2 max-w-3xl text-sm leading-7 text-gray-600 dark:text-gray-300">
            每位用户每次都会拿到一题单独的 Argon2 题目，而不是和所有人竞争同一个谜题。后端负责出题和验题，前端 worker 只负责本地搜索 nonce。
          </p>
        </div>

        <div className="grid min-w-[240px] gap-3 sm:grid-cols-2 md:grid-cols-1 xl:grid-cols-2">
          <StatCard title="今日已完成" value={`${status?.completed_today ?? 0} / ${status?.max_daily_completions ?? 5}`} />
          <StatCard title="剩余可完成" value={`${status?.remaining_today ?? 0} 次`} />
          <StatCard title="当前累计福利" value={`${status?.current_remaining_count?.toLocaleString('zh-CN') ?? '0'} 次`} />
          <StatCard title="邮箱权限状态" value={permissionStatusLabel} />
        </div>
      </div>

      {loading ? <InlineNotice tone="info" message="正在加载 PoW 福利状态..." /> : null}
      {error ? <InlineNotice tone="error" message={error} /> : null}
      {notice ? <InlineNotice tone={notice.tone} message={notice.message} /> : null}

      {!authenticated ? (
        <div className="rounded-3xl border border-dashed border-white/25 bg-white/25 px-5 py-8 text-center dark:border-white/10 dark:bg-black/15">
          <div className="mx-auto inline-flex h-12 w-12 items-center justify-center rounded-full bg-white/60 text-amber-600 dark:bg-white/10 dark:text-amber-300">
            <ShieldAlert size={22} />
          </div>
          <div className="mt-4 text-lg font-bold text-gray-900 dark:text-white">登录后开始 PoW 福利</div>
          <p className="mx-auto mt-2 max-w-2xl text-sm leading-7 text-gray-600 dark:text-gray-300">
            当前福利奖励会增加邮箱泛解析剩余次数。界面已经预留了福利类型下拉框，后续可以继续扩展到其他奖励。
          </p>
          <button
            type="button"
            onClick={onLogin}
            className="mt-5 inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-amber-500 to-orange-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-amber-600 hover:to-orange-600"
          >
            <Sparkles size={18} />
            登录后开始
          </button>
        </div>
      ) : (
        <>
          {catchAllPermission?.status !== 'approved' ? (
            <InlineNotice
              tone="info"
              message="当前福利会先累计到你的账号里。即使邮箱泛解析权限还没通过，这些次数也不会丢失，等权限开通后即可使用。"
            />
          ) : null}

          <div className="grid gap-5 lg:grid-cols-[minmax(0,1.1fr)_minmax(280px,0.9fr)]">
            <div className="space-y-4">
              <div>
                <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">福利类型</label>
                <GlassSelect
                  options={benefitOptions}
                  value={selectedBenefitKey}
                  onChange={setSelectedBenefitKey}
                  placeholder="请选择福利类型"
                  disabled={creating || solving || claiming || benefitOptions.length === 0}
                />
              </div>

              <div>
                <label className="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">挑战难度</label>
                <GlassSelect
                  options={difficultyOptions}
                  value={selectedDifficulty}
                  onChange={setSelectedDifficulty}
                  placeholder="请选择挑战难度"
                  disabled={creating || solving || claiming || difficultyOptions.length === 0}
                />
                <div className="mt-2 text-xs leading-6 text-gray-500 dark:text-gray-400">
                  当前难度按前导零 bit 数计算。难度越高，浏览器本地解题越慢，但奖励倍数也越高。
                </div>
              </div>

              <button
                type="button"
                onClick={() => void handleCreateChallenge()}
                disabled={!canGenerate}
                className="inline-flex w-full items-center justify-center gap-2 rounded-2xl bg-gradient-to-r from-amber-500 to-orange-500 px-5 py-3 font-semibold text-white shadow-lg transition hover:from-amber-600 hover:to-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {creating ? <LoaderCircle size={18} className="animate-spin" /> : <Gift size={18} />}
                {creating ? '生成题目中...' : (status?.remaining_today ?? 0) > 0 ? '生成新题目' : '今日次数已用完'}
              </button>
            </div>

            <div className="rounded-[1.75rem] border border-white/15 bg-white/35 p-5 dark:border-white/10 dark:bg-black/20">
              <div className="text-sm font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">奖励预览</div>
              <div className="mt-3 text-lg font-bold text-gray-900 dark:text-white">
                {selectedDifficultyMeta ? `${selectedDifficultyMeta.reward_multiplier}x 难度倍率` : '等待选择难度'}
              </div>
              <div className="mt-2 text-sm leading-7 text-gray-600 dark:text-gray-300">
                基础奖励固定为随机 5 到 10 次。最终发放量 = 基础奖励 × 难度倍率。
              </div>
              <div className="mt-4 grid gap-3 sm:grid-cols-2">
                <MiniStat title="基础奖励" value="5 ~ 10 次" />
                <MiniStat title="当前倍率" value={selectedDifficultyMeta ? `${selectedDifficultyMeta.reward_multiplier}x` : '--'} />
                <MiniStat title="最低发放" value={selectedDifficultyMeta ? `${(5 * selectedDifficultyMeta.reward_multiplier).toLocaleString('zh-CN')} 次` : '--'} />
                <MiniStat title="最高发放" value={selectedDifficultyMeta ? `${(10 * selectedDifficultyMeta.reward_multiplier).toLocaleString('zh-CN')} 次` : '--'} />
              </div>
            </div>
          </div>

          <div className="rounded-[1.75rem] border border-white/15 bg-white/35 p-5 dark:border-white/10 dark:bg-black/20">
            <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
              <div>
                <div className="inline-flex items-center gap-2 rounded-full bg-white/70 px-3 py-1 text-xs font-semibold text-slate-700 dark:bg-black/30 dark:text-slate-200">
                  <Cpu size={14} />
                  当前题目
                </div>
                <div className="mt-3 text-xl font-bold text-gray-900 dark:text-white">
                  {currentChallenge ? `题目 #${currentChallenge.id}` : '还没有激活题目'}
                </div>
                <div className="mt-2 text-sm leading-7 text-gray-600 dark:text-gray-300">
                  {currentChallenge
                    ? `本题奖励 ${currentChallenge.reward_quantity.toLocaleString('zh-CN')}${currentChallenge.reward_unit}，需要达到 ${currentChallenge.difficulty} 个前导零 bit。`
                    : '生成新题后，前端 worker 才会开始本地尝试 nonce。生成新题会直接覆盖旧题。'}
                </div>
              </div>

              <div className="flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={() => (solving ? handleStopSolving() : handleStartSolving())}
                  disabled={solving ? false : !canSolve}
                  className="inline-flex items-center gap-2 rounded-2xl bg-gradient-to-r from-emerald-500 to-teal-600 px-4 py-3 text-sm font-semibold text-white shadow-lg transition hover:from-emerald-600 hover:to-teal-700 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {solving ? <StopCircle size={16} /> : <Pickaxe size={16} />}
                  {solving ? '停止解题' : '启动 worker 解题'}
                </button>
                <button
                  type="button"
                  onClick={() => void loadStatus()}
                  disabled={loading || creating || claiming}
                  className="inline-flex items-center gap-2 rounded-2xl border border-white/20 bg-white/70 px-4 py-3 text-sm font-medium text-gray-900 transition hover:bg-white disabled:cursor-not-allowed disabled:opacity-60 dark:border-white/10 dark:bg-black/35 dark:text-white dark:hover:bg-black/50"
                >
                  <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
                  刷新状态
                </button>
              </div>
            </div>

            {currentChallenge ? (
              <div className="mt-5 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                <MiniStat title="基础奖励" value={`${currentChallenge.base_reward.toLocaleString('zh-CN')} 次`} />
                <MiniStat title="总奖励" value={`${currentChallenge.reward_quantity.toLocaleString('zh-CN')} 次`} />
                <MiniStat title="Argon2 参数" value={`m=${currentChallenge.argon2_memory_kib}KiB t=${currentChallenge.argon2_iterations}`} />
                <MiniStat title="创建时间" value={formatDate(currentChallenge.created_at)} />
              </div>
            ) : null}

            {solving || solveProgress ? (
              <div className="mt-5 rounded-3xl border border-emerald-300/25 bg-emerald-50/80 p-4 dark:border-emerald-700/25 dark:bg-emerald-950/20">
                <div className="text-sm font-semibold text-emerald-900 dark:text-emerald-100">浏览器解题进度</div>
                <div className="mt-3 grid gap-3 sm:grid-cols-3">
                  <MiniStat title="已尝试 nonce" value={solveProgress ? solveProgress.attempts.toLocaleString('zh-CN') : '0'} />
                  <MiniStat title="最好成绩" value={solveProgress ? `${solveProgress.bestLeadingZeroBits} bit` : '0 bit'} />
                  <MiniStat title="耗时" value={solveProgress ? formatElapsedMs(solveProgress.elapsedMs) : '0.0s'} />
                </div>
              </div>
            ) : null}
          </div>
        </>
      )}
    </GlassCard>
  );
}

function InlineNotice({ tone, message }: SectionNotice) {
  const palette = tone === 'success'
    ? 'border-emerald-300/35 bg-emerald-100/70 text-emerald-900 dark:border-emerald-700/35 dark:bg-emerald-950/30 dark:text-emerald-100'
    : tone === 'error'
      ? 'border-red-300/35 bg-red-100/70 text-red-900 dark:border-red-700/35 dark:bg-red-950/30 dark:text-red-100'
      : 'border-sky-300/35 bg-sky-100/70 text-sky-900 dark:border-sky-700/35 dark:bg-sky-950/30 dark:text-sky-100';

  return <div className={`rounded-2xl border px-4 py-3 text-sm leading-7 ${palette}`}>{message}</div>;
}

function StatCard({ title, value }: { title: string; value: string }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/35 p-4 dark:border-white/10 dark:bg-black/20">
      <div className="text-xs font-semibold uppercase tracking-[0.18em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className="mt-2 text-base font-semibold text-gray-900 dark:text-white">{value}</div>
    </div>
  );
}

function MiniStat({ title, value }: { title: string; value: string }) {
  return (
    <div className="rounded-2xl border border-white/15 bg-white/40 p-3 dark:border-white/10 dark:bg-black/20">
      <div className="text-xs font-semibold uppercase tracking-[0.16em] text-gray-500 dark:text-gray-400">{title}</div>
      <div className="mt-2 text-sm font-semibold text-gray-900 dark:text-white">{value}</div>
    </div>
  );
}

function formatDate(value?: string): string {
  if (!value) return '暂无';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function formatElapsedMs(value: number): string {
  return `${(value / 1000).toFixed(1)}s`;
}

function describePermissionStatus(status: UserPermission['status']): string {
  switch (status) {
    case 'approved':
      return '已通过';
    case 'pending':
      return '待审核';
    case 'rejected':
      return '未通过';
    default:
      return '尚未申请';
  }
}

function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message;
  if (error instanceof Error && error.message.trim() !== '') return error.message;
  return fallback;
}
