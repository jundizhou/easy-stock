import type { BillboardSeatKind } from '../lib/billboard';

export type BillboardSeatMapping = {
	/** 可匹配的席位名称片段，优先使用完整营业部名称。 */
	keywords: string[];
	/** 展示标签，不代表平台对真实操盘人的官方确认。 */
	label: string;
	kind: Exclude<BillboardSeatKind, 'unknown'>;
	confidence: 'high' | 'medium' | 'low';
	sources: Array<'eastmoney' | 'ths' | 'kaipanla' | 'market'>;
	note: string;
};

/**
 * 龙虎榜席位辅助映射表。
 *
 * 三个平台的口径并不相同：
 * - 东方财富提供原始营业部/“机构专用”字段；
 * - 同花顺额外提供“知名游资、敢死队、游资上榜”等统计标签；
 * - 开盘啦当前公开页面主要是涨停与题材数据，未发现可稳定复用的席位身份字段。
 *
 * 因此这里把“原始事实”和“市场常用映射”分开：confidence 仅表示匹配可靠度，
 * 不表示平台或监管机构确认了真实操盘人身份。
 */
export const BILLBOARD_SEAT_MAPPINGS: BillboardSeatMapping[] = [
	{
		keywords: ['机构专用', '机构席位', '机构'], label: '机构', kind: 'institution', confidence: 'high', sources: ['eastmoney', 'ths'],
		note: '东方财富原始席位名称或同花顺机构席位分类明确标识。',
	},
	{
		keywords: ['陈小群', '大连黄河路证券营业部', '大连黄河路'], label: '游资 · 陈小群', kind: 'trader', confidence: 'low', sources: ['ths', 'market'],
		note: '市场公开资料常用映射；需以当期席位名称和多日轨迹交叉验证，不能据此确认真实操盘人。',
	},
	{
		keywords: ['中国银河证券股份有限公司绍兴证券营业部', '银河证券绍兴证券营业部'], label: '游资 · 赵老哥', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场长期使用的常用席位映射；不同阶段可能发生席位迁移。',
	},
	{
		keywords: ['上海宛平南路证券营业部', '华鑫证券上海宛平南路'], label: '游资 · 炒股养家', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场长期使用的常用席位映射；仅作为辅助标签。',
	},
	{
		keywords: ['南京太平南路证券营业部', '国泰君安南京太平南路'], label: '游资 · 作手新一', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场常用席位映射；同一席位可能被不同资金使用。',
	},
	{
		keywords: ['财通证券股份有限公司杭州上塘路证券营业部', '杭州上塘路证券营业部', '上塘路'], label: '游资 · 上塘路', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场常用席位映射；不等同于操盘人身份核验。',
	},
	{
		keywords: ['上海江苏路证券营业部', '国泰海通证券上海长宁区江苏路证券营业部'], label: '游资 · 章盟主', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场常用席位映射；需结合历史上榜轨迹判断。',
	},
	{
		keywords: ['上海溧阳路证券营业部', '中信证券上海溧阳路'], label: '游资 · 孙哥', kind: 'trader', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '市场常用席位映射；仅用于阅读龙虎榜结构。',
	},
	{
		keywords: ['东方财富证券股份有限公司拉萨', '拉萨东环路', '拉萨团结路', '拉萨金融城南环路', '拉萨北京中路'], label: '活跃资金 · 拉萨席位集群', kind: 'retail', confidence: 'medium', sources: ['eastmoney', 'ths', 'market'],
		note: '同花顺与市场常将拉萨多家营业部作为活跃散户/跟风资金集群观察，不代表每笔交易都由散户完成。',
	},
	{
		keywords: ['量化交易', '量化席位', '算法交易'], label: '量化', kind: 'quant', confidence: 'medium', sources: ['ths', 'market'],
		note: '仅当原始席位或平台文本明确出现量化/算法标签时使用。',
	},
	{
		keywords: ['华鑫证券上海分公司', '华鑫证券股份有限公司上海分公司'], label: '量化/活跃资金', kind: 'quant', confidence: 'low', sources: ['eastmoney', 'ths', 'market'],
		note: '市场常见量化席位候选，不能仅凭营业部名称确认资金类型。',
	},
];

export const BILLBOARD_SOURCE_NOTES = {
	eastmoney: {
	name: '东方财富',
	url: 'https://data.eastmoney.com/stock/lhb.html',
	confirmed: ['机构专用原始席位名', '买卖营业部名称', '上榜原因', '买卖金额'],
	status: '已接入：easy-stock 当前龙虎榜明细接口直接使用东方财富 HSF10 数据。',
	},
	ths: {
	name: '同花顺',
	url: 'https://data.10jqka.com.cn/market/longhu/',
	confirmed: ['机构参与', '敢死队上榜', '游资上榜', '知名营业部/热门营业部统计'],
	status: '已核对公开页面分类；目前作为映射表和口径参考，不直接抓取其登录态数据。',
	},
	kaipanla: {
	name: '开盘啦',
	url: 'https://www.kaipanla.com/',
	confirmed: ['涨停池', '连板梯队', '题材与龙头'],
	status: '当前项目已接入其涨停/连板/题材数据；公开页面未发现可稳定复用的龙虎榜席位身份字段，席位映射暂标为待确认。',
	},
} as const;
