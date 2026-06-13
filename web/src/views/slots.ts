export interface SlotDef {
  title: string
  desc: string
  milestone: 'M3' | 'M4' | 'M5' | 'M6' | 'M7'
  span?: 1 | 2 // 栅格跨度（2 列布局）
  live?: 'narrative' | 'overnight' | 'gaps' | 'movers' | 'sentiment' // 已上线槽：渲染真实组件而非占位卡
}

export const viewSlots: Record<string, SlotDef[]> = {
  premarket: [
    { title: '隔夜传导链', desc: '东京→台北→欧洲→美期 接力涨跌与传导强度', milestone: 'M4', span: 2, live: 'overnight' },
    { title: '隐含开盘 + 跳空回补', desc: '期货隐含开盘价与历史跳空回补概率', milestone: 'M4', live: 'gaps' },
    { title: '盘前异动', desc: '盘前成交异动与量比归因', milestone: 'M4', live: 'movers' },
    { title: '情绪定位', desc: 'P/C 比、VIX 期限升水的情绪坐标', milestone: 'M4', live: 'sentiment' },
  ],
  intraday: [
    { title: '盘中异动', desc: '实时异动与板块轮动', milestone: 'M4', span: 2 },
    { title: '板块热力', desc: '行业板块涨跌热力图', milestone: 'M4' },
    { title: '叙事流', desc: '检查点叙事与判断登记（agent 填槽）', milestone: 'M3', live: 'narrative' },
  ],
  postclose: [
    { title: '财报速递', desc: '盘后财报 vs 共识速览', milestone: 'M5', span: 2 },
    { title: '要点时间轴', desc: '财报电话会要点时间轴', milestone: 'M5' },
    { title: 'Read-across 传导', desc: '同业/供应链传导图谱', milestone: 'M5' },
    { title: '全天合并异动', desc: '正股+盘后合并异动列表', milestone: 'M5' },
  ],
  event: [
    { title: '利率路径定价', desc: '事前冻结 vs T+0 重定价对比', milestone: 'M6', span: 2 },
    { title: '声明措辞 Diff', desc: 'FOMC 声明逐句对比与鹰鸽分类', milestone: 'M6' },
    { title: '历史事件日模式', desc: '同类事件日的资产路径统计', milestone: 'M6' },
    { title: '资产敏感度矩阵', desc: '事件冲击下各资产 beta 矩阵', milestone: 'M6' },
  ],
  shock: [
    { title: 'Regime 触发器', desc: 'VIX σ + 跨资产确认的触发状态', milestone: 'M7', span: 2 },
    { title: '冲击指纹', desc: '供给/需求/流动性/政策 四类指纹分类', milestone: 'M7' },
    { title: '历史类比', desc: '与历史冲击日的相似度匹配', milestone: 'M7' },
    { title: '流动性状态', desc: '价差/深度近似的流动性仪表', milestone: 'M7' },
  ],
}
