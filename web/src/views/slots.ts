export interface SlotDef {
  title: string
  desc: string
  milestone: 'M3' | 'M4' | 'M5' | 'M6' | 'M7'
  span?: 1 | 2 // 栅格跨度（2 列布局）
  live?:
    | 'narrative'
    | 'overnight'
    | 'gaps'
    | 'movers'
    | 'sentiment'
    | 'intraday-movers'
    | 'intraday-sector-heatmap'
    | 'postclose-earnings'
    | 'postclose-timeline'
    | 'postclose-read-across'
    | 'postclose-movers'
    | 'event-rates'
    | 'event-diff'
    | 'event-patterns'
    | 'event-sensitivity'
    | 'shock-regime'
    | 'shock-fingerprint'
    | 'shock-analogs'
    | 'shock-liquidity' // 已上线槽：渲染真实组件而非占位卡
}

export const viewSlots: Record<string, SlotDef[]> = {
  premarket: [
    { title: '隔夜传导链', desc: '东京→台北→欧洲→美期 接力涨跌与传导强度', milestone: 'M4', span: 2, live: 'overnight' },
    { title: '隐含开盘 + 跳空回补', desc: '期货隐含开盘价与历史跳空回补概率', milestone: 'M4', live: 'gaps' },
    { title: '盘前异动', desc: '盘前成交异动与量比归因', milestone: 'M4', live: 'movers' },
    { title: '情绪定位', desc: 'P/C 比、VIX 期限升水的情绪坐标', milestone: 'M4', live: 'sentiment' },
  ],
  intraday: [
    { title: '叙事流', desc: '检查点叙事与判断登记（agent 填槽）', milestone: 'M3', live: 'narrative' },
    { title: '盘中异动', desc: '实时涨跌异动与自选股标记', milestone: 'M7', live: 'intraday-movers' },
    { title: '板块热力', desc: '盘中样本按板块聚合的方向与强度', milestone: 'M7', live: 'intraday-sector-heatmap' },
  ],
  postclose: [
    { title: '财报速递', desc: '盘后财报 vs 共识速览', milestone: 'M5', span: 2, live: 'postclose-earnings' },
    { title: '要点时间轴', desc: '财报电话会要点时间轴', milestone: 'M5', live: 'postclose-timeline' },
    { title: 'Read-across 传导', desc: '同业/供应链传导图谱', milestone: 'M5', live: 'postclose-read-across' },
    { title: '全天合并异动', desc: '正股+盘后合并异动列表', milestone: 'M5', live: 'postclose-movers' },
  ],
  event: [
    { title: '利率路径定价', desc: '事前冻结 vs T+0 重定价对比', milestone: 'M6', span: 2, live: 'event-rates' },
    { title: '声明措辞 Diff', desc: 'FOMC 声明逐句对比与鹰鸽分类', milestone: 'M6', live: 'event-diff' },
    { title: '历史事件日模式', desc: '同类事件日的资产路径统计', milestone: 'M6', live: 'event-patterns' },
    { title: '资产敏感度矩阵', desc: '事件冲击下各资产 beta 矩阵', milestone: 'M6', live: 'event-sensitivity' },
  ],
  shock: [
    { title: 'Regime 触发器', desc: 'VIX σ + 跨资产确认的触发状态', milestone: 'M7', span: 2, live: 'shock-regime' },
    { title: '冲击指纹', desc: '供给/需求/流动性/政策 四类指纹分类', milestone: 'M7', live: 'shock-fingerprint' },
    { title: '历史类比', desc: '与历史冲击日的相似度匹配', milestone: 'M7', live: 'shock-analogs' },
    { title: '流动性状态', desc: '价差/深度近似的流动性仪表', milestone: 'M7', live: 'shock-liquidity' },
  ],
}
