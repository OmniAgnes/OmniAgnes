import {
  ArrowRightIcon,
  BotIcon,
  CalendarClockIcon,
  CpuIcon,
  GiftIcon,
  ReceiptTextIcon,
  SparklesIcon,
  WrenchIcon,
  ZapIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import type { Site } from "@/lib/api"

const go = (h: string) => {
  location.hash = h
}

const FEATURES = [
  {
    icon: CpuIcon,
    title: "多模型自由切换",
    desc: "OpenAI、Claude、Gemini、本地 Ollama 一处配置；模型倍率、供应商启停后台可控。",
  },
  {
    icon: ZapIcon,
    title: "内置模型即开即用",
    desc: "无需自备 API Key，登录后选择内置模型直接开聊，按 token 用量计费。",
  },
  {
    icon: WrenchIcon,
    title: "MCP 工具生态",
    desc: "文件读写、内置浏览器、Word/Excel/PPT 生成，工具调用过程全程可见。",
  },
  {
    icon: CalendarClockIcon,
    title: "计划任务",
    desc: "按天定时自动执行，错过时点 24 小时内自动补跑，结果站内回看。",
  },
  {
    icon: ReceiptTextIcon,
    title: "用量透明可控",
    desc: "每次请求的 token 与积分明细逐条可查，限免模型 0 积分畅用。",
  },
  {
    icon: GiftIcon,
    title: "每日领取积分",
    desc: "打开客户端自动领取当日积分，管理员可随时赠送调整。",
  },
]

const STEPS = [
  { title: "注册登录", desc: "手机号验证码一键登录，未注册自动创建账号" },
  { title: "领取积分", desc: "登录即领当日积分，新号马上可用" },
  { title: "开始对话", desc: "挑选模型下达任务：写代码、查资料、做文档" },
]

/** 桌面端应用窗口模拟：纯 CSS 绘制，不依赖图片资源 */
function AppMock() {
  return (
    <div className="border-border/60 bg-card relative mx-auto mt-12 max-w-3xl overflow-hidden rounded-2xl border text-left shadow-xl">
      <div className="border-border/60 flex items-center gap-1.5 border-b px-4 py-2.5">
        <span className="size-2.5 rounded-full bg-red-400" />
        <span className="size-2.5 rounded-full bg-amber-400" />
        <span className="size-2.5 rounded-full bg-emerald-400" />
        <span className="text-muted-foreground ml-3 text-xs">omni Agent</span>
      </div>
      <div className="flex flex-col gap-3 p-5 text-sm">
        <div className="bg-primary text-primary-foreground ml-auto max-w-[75%] rounded-2xl rounded-br-sm px-4 py-2">
          帮我把这份周报整理成一页 PPT，配上图表
        </div>
        <div className="border-border/60 bg-muted/50 flex max-w-[80%] items-center gap-2 rounded-xl border px-3 py-2 text-xs text-muted-foreground">
          <WrenchIcon className="size-3.5" />
          pptx · 生成中…
        </div>
        <div className="bg-muted/50 max-w-[80%] rounded-2xl rounded-bl-sm px-4 py-2.5">
          已完成：封面 + 3 个章节 + 数据图表，文件已就绪，可直接下载。
        </div>
        <div className="flex items-center gap-1 pl-1">
          <span className="bg-muted-foreground/40 size-1.5 animate-bounce rounded-full" />
          <span className="bg-muted-foreground/40 size-1.5 animate-bounce rounded-full [animation-delay:150ms]" />
          <span className="bg-muted-foreground/40 size-1.5 animate-bounce rounded-full [animation-delay:300ms]" />
        </div>
      </div>
    </div>
  )
}

/** 推广首页（#/）：产品宣传 + 登录/进入控制台入口。未登录与已登录共用，按钮随之切换。 */
export function Landing({ site, loggedIn }: { site: Site; loggedIn: boolean }) {
  const title = site.title || "omni server"
  const cta = () => go(loggedIn ? "/me" : "/login")
  const scrollFeatures = () => document.getElementById("features")?.scrollIntoView({ behavior: "smooth" })

  return (
    <div className="flex min-h-screen flex-col">
      {/* 顶部导航 */}
      <header className="border-border/60 bg-background sticky top-0 z-40 border-b">
        <div className="mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="bg-primary flex size-7 items-center justify-center rounded-lg text-white [&_svg]:size-4">
              <BotIcon />
            </span>
            <span className="text-base font-semibold tracking-tight">{title}</span>
          </div>
          <Button size="sm" onClick={cta}>
            {loggedIn ? "进入控制台" : "登录 / 注册"}
          </Button>
        </div>
      </header>

      <main className="flex-1">
        {/* Hero */}
        <section className="from-primary/8 via-background to-background bg-gradient-to-b">
          <div className="mx-auto max-w-6xl px-4 pt-16 text-center md:pt-24">
            <span className="border-primary/20 bg-primary/5 text-primary inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium">
              <SparklesIcon className="size-3.5" />
              v1.0.0 正式发布
            </span>
            <h1 className="mx-auto mt-5 max-w-3xl text-4xl font-bold tracking-tight md:text-5xl md:leading-[1.15]">
              把 AI 变成你的<span className="text-primary">日常工作台</span>
            </h1>
            <p className="text-muted-foreground mx-auto mt-5 max-w-2xl text-base leading-relaxed md:text-lg">
              {title} 桌面端 + 云端账号：多模型切换、MCP 工具、计划任务一站俱全；
              内置模型登录即用，积分按量计费，每一笔都透明可查。
            </p>
            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Button size="lg" onClick={cta}>
                {loggedIn ? "进入控制台" : "立即开始"}
                <ArrowRightIcon className="size-4" />
              </Button>
              <Button size="lg" variant="outline" onClick={scrollFeatures}>
                了解功能
              </Button>
            </div>
            <AppMock />
          </div>
        </section>

        {/* 功能特性 */}
        <section id="features" className="mx-auto max-w-6xl px-4 py-16 md:py-20">
          <h2 className="text-center text-2xl font-bold md:text-3xl">为日常任务而生</h2>
          <p className="text-muted-foreground mt-2 text-center">从一次对话到一整套自动化，覆盖完整工作流</p>
          <div className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {FEATURES.map((f) => (
              <div
                key={f.title}
                className="border-border/60 bg-card rounded-2xl border p-5 transition-shadow hover:shadow-md"
              >
                <span className="bg-primary/10 text-primary flex size-10 items-center justify-center rounded-xl [&_svg]:size-5">
                  <f.icon />
                </span>
                <h3 className="mt-3 font-semibold">{f.title}</h3>
                <p className="text-muted-foreground mt-1.5 text-sm leading-relaxed">{f.desc}</p>
              </div>
            ))}
          </div>
        </section>

        {/* 三步上手 */}
        <section className="border-border/60 bg-muted/40 border-y">
          <div className="mx-auto max-w-6xl px-4 py-16">
            <h2 className="text-center text-2xl font-bold md:text-3xl">三步上手</h2>
            <div className="mt-10 grid gap-6 md:grid-cols-3">
              {STEPS.map((s, i) => (
                <div key={s.title} className="flex gap-4">
                  <span className="bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-full font-semibold">
                    {i + 1}
                  </span>
                  <div>
                    <h3 className="font-semibold">{s.title}</h3>
                    <p className="text-muted-foreground mt-1 text-sm">{s.desc}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* 底部 CTA */}
        <section className="mx-auto max-w-6xl px-4 py-16">
          <div className="from-primary to-primary/75 rounded-3xl bg-gradient-to-r px-6 py-12 text-center text-white md:py-16">
            <h2 className="text-2xl font-bold md:text-3xl">现在就试试 {title}</h2>
            <p className="mt-2 text-white/85">注册即领当日积分，内置模型即刻开聊。</p>
            <Button size="lg" className="bg-white text-primary hover:bg-white/90 mt-6" onClick={cta}>
              {loggedIn ? "进入控制台" : "免费注册"}
            </Button>
          </div>
        </section>
      </main>

      {/* 页脚 */}
      <footer className="border-border/60 border-t">
        <div className="text-muted-foreground mx-auto flex max-w-6xl flex-col items-center justify-between gap-2 px-4 py-6 text-sm md:flex-row">
          <div className="flex items-center gap-2">
            <span className="bg-primary flex size-5 items-center justify-center rounded text-white [&_svg]:size-3">
              <BotIcon />
            </span>
            {title}
          </div>
          {site.footer && <span>{site.footer}</span>}
        </div>
      </footer>
    </div>
  )
}
