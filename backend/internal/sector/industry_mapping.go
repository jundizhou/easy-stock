package sector

import "strings"

// radarIndustryMapping keeps provider-specific industry identities aligned.
// The catalog covers all 124 Tencent second-level industries returned by the
// live industry ranking on 2026-08-27 and their exact EastMoney industry board.
type radarIndustryMapping struct {
	TencentCode        string
	TencentName        string
	EastMoneyBoardCode string
	EastMoneyBoardName string
}

var radarIndustryMappings = []radarIndustryMapping{
	{TencentCode: "pt01801039", TencentName: "非金属材料Ⅱ", EastMoneyBoardCode: "BK1020", EastMoneyBoardName: "非金属材料Ⅱ"},
	{TencentCode: "pt01801086", TencentName: "电子化学品Ⅱ", EastMoneyBoardCode: "BK1039", EastMoneyBoardName: "电子化学品Ⅱ"},
	{TencentCode: "pt01801712", TencentName: "玻璃玻纤", EastMoneyBoardCode: "BK0546", EastMoneyBoardName: "玻璃玻纤"},
	{TencentCode: "pt01801083", TencentName: "元件", EastMoneyBoardCode: "BK0459", EastMoneyBoardName: "元件"},
	{TencentCode: "pt01801082", TencentName: "其他电子Ⅱ", EastMoneyBoardCode: "BK1223", EastMoneyBoardName: "其他电子Ⅱ"},
	{TencentCode: "pt01801081", TencentName: "半导体", EastMoneyBoardCode: "BK1036", EastMoneyBoardName: "半导体"},
	{TencentCode: "pt01801016", TencentName: "种植业", EastMoneyBoardCode: "BK1261", EastMoneyBoardName: "种植业"},
	{TencentCode: "pt01801054", TencentName: "小金属", EastMoneyBoardCode: "BK1027", EastMoneyBoardName: "小金属"},
	{TencentCode: "pt01801102", TencentName: "通信设备", EastMoneyBoardCode: "BK0448", EastMoneyBoardName: "通信设备"},
	{TencentCode: "pt01801036", TencentName: "塑料", EastMoneyBoardCode: "BK0454", EastMoneyBoardName: "塑料"},
	{TencentCode: "pt01801072", TencentName: "通用设备", EastMoneyBoardCode: "BK0545", EastMoneyBoardName: "通用设备"},
	{TencentCode: "pt01801084", TencentName: "光学光电子", EastMoneyBoardCode: "BK1038", EastMoneyBoardName: "光学光电子"},
	{TencentCode: "pt01801745", TencentName: "军工电子Ⅱ", EastMoneyBoardCode: "BK1233", EastMoneyBoardName: "军工电子Ⅱ"},
	{TencentCode: "pt01801051", TencentName: "金属新材料", EastMoneyBoardCode: "BK1288", EastMoneyBoardName: "金属新材料"},
	{TencentCode: "pt01801032", TencentName: "化学纤维", EastMoneyBoardCode: "BK0471", EastMoneyBoardName: "化学纤维"},
	{TencentCode: "pt01801012", TencentName: "农产品加工", EastMoneyBoardCode: "BK1256", EastMoneyBoardName: "农产品加工"},
	{TencentCode: "pt01801085", TencentName: "消费电子", EastMoneyBoardCode: "BK1037", EastMoneyBoardName: "消费电子"},
	{TencentCode: "pt01801078", TencentName: "自动化设备", EastMoneyBoardCode: "BK1237", EastMoneyBoardName: "自动化设备"},
	{TencentCode: "pt01801231", TencentName: "综合Ⅱ", EastMoneyBoardCode: "BK0539", EastMoneyBoardName: "综合Ⅱ"},
	{TencentCode: "pt01801053", TencentName: "贵金属", EastMoneyBoardCode: "BK0732", EastMoneyBoardName: "贵金属"},
	{TencentCode: "pt01801101", TencentName: "计算机设备", EastMoneyBoardCode: "BK0735", EastMoneyBoardName: "计算机设备"},
	{TencentCode: "pt01801963", TencentName: "炼化及贸易", EastMoneyBoardCode: "BK1274", EastMoneyBoardName: "炼化及贸易"},
	{TencentCode: "pt01801951", TencentName: "煤炭开采", EastMoneyBoardCode: "BK1250", EastMoneyBoardName: "煤炭开采"},
	{TencentCode: "pt01801743", TencentName: "地面兵装Ⅱ", EastMoneyBoardCode: "BK1229", EastMoneyBoardName: "地面兵装Ⅱ"},
	{TencentCode: "pt01801034", TencentName: "化学制品", EastMoneyBoardCode: "BK0538", EastMoneyBoardName: "化学制品"},
	{TencentCode: "pt01801104", TencentName: "软件开发", EastMoneyBoardCode: "BK0737", EastMoneyBoardName: "软件开发"},
	{TencentCode: "pt01801112", TencentName: "黑色家电", EastMoneyBoardCode: "BK1241", EastMoneyBoardName: "黑色家电"},
	{TencentCode: "pt01801103", TencentName: "IT服务Ⅱ", EastMoneyBoardCode: "BK1238", EastMoneyBoardName: "IT服务Ⅱ"},
	{TencentCode: "pt01801033", TencentName: "化学原料", EastMoneyBoardCode: "BK1019", EastMoneyBoardName: "化学原料"},
	{TencentCode: "pt01801994", TencentName: "教育", EastMoneyBoardCode: "BK0740", EastMoneyBoardName: "教育"},
	{TencentCode: "pt01801074", TencentName: "专用设备", EastMoneyBoardCode: "BK0910", EastMoneyBoardName: "专用设备"},
	{TencentCode: "pt01801115", TencentName: "照明设备Ⅱ", EastMoneyBoardCode: "BK1245", EastMoneyBoardName: "照明设备Ⅱ"},
	{TencentCode: "pt01801116", TencentName: "家电零部件Ⅱ", EastMoneyBoardCode: "BK1242", EastMoneyBoardName: "家电零部件Ⅱ"},
	{TencentCode: "pt01801037", TencentName: "橡胶", EastMoneyBoardCode: "BK1018", EastMoneyBoardName: "橡胶"},
	{TencentCode: "pt01801741", TencentName: "航天装备Ⅱ", EastMoneyBoardCode: "BK1232", EastMoneyBoardName: "航天装备Ⅱ"},
	{TencentCode: "pt01801017", TencentName: "养殖业", EastMoneyBoardCode: "BK1259", EastMoneyBoardName: "养殖业"},
	{TencentCode: "pt01801952", TencentName: "焦炭Ⅱ", EastMoneyBoardCode: "BK1249", EastMoneyBoardName: "焦炭Ⅱ"},
	{TencentCode: "pt01801972", TencentName: "环保设备Ⅱ", EastMoneyBoardCode: "BK1234", EastMoneyBoardName: "环保设备Ⅱ"},
	{TencentCode: "pt01801218", TencentName: "专业服务", EastMoneyBoardCode: "BK1043", EastMoneyBoardName: "专业服务"},
	{TencentCode: "pt01801962", TencentName: "油服工程", EastMoneyBoardCode: "BK1275", EastMoneyBoardName: "油服工程"},
	{TencentCode: "pt01801742", TencentName: "航空装备Ⅱ", EastMoneyBoardCode: "BK1231", EastMoneyBoardName: "航空装备Ⅱ"},
	{TencentCode: "pt01801014", TencentName: "饲料", EastMoneyBoardCode: "BK1258", EastMoneyBoardName: "饲料"},
	{TencentCode: "pt01801151", TencentName: "化学制药", EastMoneyBoardCode: "BK0465", EastMoneyBoardName: "化学制药"},
	{TencentCode: "pt01801733", TencentName: "其他电源设备Ⅱ", EastMoneyBoardCode: "BK1034", EastMoneyBoardName: "其他电源设备Ⅱ"},
	{TencentCode: "pt01801056", TencentName: "能源金属", EastMoneyBoardCode: "BK1015", EastMoneyBoardName: "能源金属"},
	{TencentCode: "pt01801015", TencentName: "渔业", EastMoneyBoardCode: "BK1260", EastMoneyBoardName: "渔业"},
	{TencentCode: "pt01801038", TencentName: "农化制品", EastMoneyBoardCode: "BK0731", EastMoneyBoardName: "农化制品"},
	{TencentCode: "pt01801093", TencentName: "汽车零部件", EastMoneyBoardCode: "BK0481", EastMoneyBoardName: "汽车零部件"},
	{TencentCode: "pt01801193", TencentName: "证券Ⅱ", EastMoneyBoardCode: "BK0473", EastMoneyBoardName: "证券Ⅱ"},
	{TencentCode: "pt01801731", TencentName: "电机Ⅱ", EastMoneyBoardCode: "BK1030", EastMoneyBoardName: "电机Ⅱ"},
	{TencentCode: "pt01801141", TencentName: "包装印刷", EastMoneyBoardCode: "BK1265", EastMoneyBoardName: "包装印刷"},
	{TencentCode: "pt01801077", TencentName: "工程机械", EastMoneyBoardCode: "BK0739", EastMoneyBoardName: "工程机械"},
	{TencentCode: "pt01801744", TencentName: "航海装备Ⅱ", EastMoneyBoardCode: "BK1230", EastMoneyBoardName: "航海装备Ⅱ"},
	{TencentCode: "pt01801765", TencentName: "广告营销", EastMoneyBoardCode: "BK1220", EastMoneyBoardName: "广告营销"},
	{TencentCode: "pt01801206", TencentName: "互联网电商", EastMoneyBoardCode: "BK1268", EastMoneyBoardName: "互联网电商"},
	{TencentCode: "pt01801124", TencentName: "食品加工", EastMoneyBoardCode: "BK1280", EastMoneyBoardName: "食品加工"},
	{TencentCode: "pt01801131", TencentName: "纺织制造", EastMoneyBoardCode: "BK1224", EastMoneyBoardName: "纺织制造"},
	{TencentCode: "pt01801127", TencentName: "饮料乳品", EastMoneyBoardCode: "BK1282", EastMoneyBoardName: "饮料乳品"},
	{TencentCode: "pt01801155", TencentName: "中药Ⅱ", EastMoneyBoardCode: "BK1040", EastMoneyBoardName: "中药Ⅱ"},
	{TencentCode: "pt01801713", TencentName: "装修建材", EastMoneyBoardCode: "BK0476", EastMoneyBoardName: "装修建材"},
	{TencentCode: "pt01801724", TencentName: "专业工程", EastMoneyBoardCode: "BK1248", EastMoneyBoardName: "专业工程"},
	{TencentCode: "pt01801153", TencentName: "医疗器械", EastMoneyBoardCode: "BK1041", EastMoneyBoardName: "医疗器械"},
	{TencentCode: "pt01801736", TencentName: "风电设备", EastMoneyBoardCode: "BK1032", EastMoneyBoardName: "风电设备"},
	{TencentCode: "pt01801202", TencentName: "贸易Ⅱ", EastMoneyBoardCode: "BK0484", EastMoneyBoardName: "贸易Ⅱ"},
	{TencentCode: "pt01801764", TencentName: "游戏Ⅱ", EastMoneyBoardCode: "BK1046", EastMoneyBoardName: "游戏Ⅱ"},
	{TencentCode: "pt01801092", TencentName: "汽车服务", EastMoneyBoardCode: "BK1016", EastMoneyBoardName: "汽车服务"},
	{TencentCode: "pt01801191", TencentName: "多元金融", EastMoneyBoardCode: "BK0738", EastMoneyBoardName: "多元金融"},
	{TencentCode: "pt01801971", TencentName: "环境治理", EastMoneyBoardCode: "BK1235", EastMoneyBoardName: "环境治理"},
	{TencentCode: "pt01801163", TencentName: "燃气Ⅱ", EastMoneyBoardCode: "BK1028", EastMoneyBoardName: "燃气Ⅱ"},
	{TencentCode: "pt01801203", TencentName: "一般零售", EastMoneyBoardCode: "BK0482", EastMoneyBoardName: "一般零售"},
	{TencentCode: "pt01801133", TencentName: "饰品", EastMoneyBoardCode: "BK0734", EastMoneyBoardName: "饰品"},
	{TencentCode: "pt01801767", TencentName: "数字媒体", EastMoneyBoardCode: "BK1221", EastMoneyBoardName: "数字媒体"},
	{TencentCode: "pt01801055", TencentName: "工业金属", EastMoneyBoardCode: "BK1287", EastMoneyBoardName: "工业金属"},
	{TencentCode: "pt01801722", TencentName: "装修装饰Ⅱ", EastMoneyBoardCode: "BK0725", EastMoneyBoardName: "装修装饰Ⅱ"},
	{TencentCode: "pt01801711", TencentName: "水泥", EastMoneyBoardCode: "BK0424", EastMoneyBoardName: "水泥"},
	{TencentCode: "pt01801204", TencentName: "专业连锁Ⅱ", EastMoneyBoardCode: "BK1270", EastMoneyBoardName: "专业连锁Ⅱ"},
	{TencentCode: "pt01801181", TencentName: "房地产开发", EastMoneyBoardCode: "BK0451", EastMoneyBoardName: "房地产开发"},
	{TencentCode: "pt01801152", TencentName: "生物制品", EastMoneyBoardCode: "BK1044", EastMoneyBoardName: "生物制品"},
	{TencentCode: "pt01801726", TencentName: "工程咨询服务Ⅱ", EastMoneyBoardCode: "BK0726", EastMoneyBoardName: "工程咨询服务Ⅱ"},
	{TencentCode: "pt01801223", TencentName: "通信服务", EastMoneyBoardCode: "BK0736", EastMoneyBoardName: "通信服务"},
	{TencentCode: "pt01801156", TencentName: "医疗服务", EastMoneyBoardCode: "BK0727", EastMoneyBoardName: "医疗服务"},
	{TencentCode: "pt01801096", TencentName: "商用车", EastMoneyBoardCode: "BK1264", EastMoneyBoardName: "商用车"},
	{TencentCode: "pt01801076", TencentName: "轨交设备Ⅱ", EastMoneyBoardCode: "BK1236", EastMoneyBoardName: "轨交设备Ⅱ"},
	{TencentCode: "pt01801992", TencentName: "航运港口", EastMoneyBoardCode: "BK0450", EastMoneyBoardName: "航运港口"},
	{TencentCode: "pt01801043", TencentName: "冶钢原料", EastMoneyBoardCode: "BK1228", EastMoneyBoardName: "冶钢原料"},
	{TencentCode: "pt01801018", TencentName: "动物保健Ⅱ", EastMoneyBoardCode: "BK1254", EastMoneyBoardName: "动物保健Ⅱ"},
	{TencentCode: "pt01801143", TencentName: "造纸", EastMoneyBoardCode: "BK1267", EastMoneyBoardName: "造纸"},
	{TencentCode: "pt01801183", TencentName: "房地产服务", EastMoneyBoardCode: "BK1045", EastMoneyBoardName: "房地产服务"},
	{TencentCode: "pt01801128", TencentName: "休闲食品", EastMoneyBoardCode: "BK1281", EastMoneyBoardName: "休闲食品"},
	{TencentCode: "pt01801132", TencentName: "服装家纺", EastMoneyBoardCode: "BK1225", EastMoneyBoardName: "服装家纺"},
	{TencentCode: "pt01801993", TencentName: "旅游及景区", EastMoneyBoardCode: "BK1272", EastMoneyBoardName: "旅游及景区"},
	{TencentCode: "pt01801881", TencentName: "摩托车及其他", EastMoneyBoardCode: "BK1263", EastMoneyBoardName: "摩托车及其他"},
	{TencentCode: "pt01801981", TencentName: "个护用品", EastMoneyBoardCode: "BK1251", EastMoneyBoardName: "个护用品"},
	{TencentCode: "pt01801766", TencentName: "影视院线", EastMoneyBoardCode: "BK1222", EastMoneyBoardName: "影视院线"},
	{TencentCode: "pt01801178", TencentName: "物流", EastMoneyBoardCode: "BK0422", EastMoneyBoardName: "物流"},
	{TencentCode: "pt01801161", TencentName: "电力", EastMoneyBoardCode: "BK0428", EastMoneyBoardName: "电力"},
	{TencentCode: "pt01801045", TencentName: "特钢Ⅱ", EastMoneyBoardCode: "BK1227", EastMoneyBoardName: "特钢Ⅱ"},
	{TencentCode: "pt01801145", TencentName: "文娱用品", EastMoneyBoardCode: "BK1266", EastMoneyBoardName: "文娱用品"},
	{TencentCode: "pt01801982", TencentName: "化妆品", EastMoneyBoardCode: "BK1252", EastMoneyBoardName: "化妆品"},
	{TencentCode: "pt01801194", TencentName: "保险Ⅱ", EastMoneyBoardCode: "BK0474", EastMoneyBoardName: "保险Ⅱ"},
	{TencentCode: "pt01801044", TencentName: "普钢", EastMoneyBoardCode: "BK1226", EastMoneyBoardName: "普钢"},
	{TencentCode: "pt01801737", TencentName: "电池", EastMoneyBoardCode: "BK1033", EastMoneyBoardName: "电池"},
	{TencentCode: "pt01801142", TencentName: "家居用品", EastMoneyBoardCode: "BK0440", EastMoneyBoardName: "家居用品"},
	{TencentCode: "pt01801154", TencentName: "医药商业", EastMoneyBoardCode: "BK1042", EastMoneyBoardName: "医药商业"},
	{TencentCode: "pt01801785", TencentName: "农商行Ⅱ", EastMoneyBoardCode: "BK1612", EastMoneyBoardName: "农商行Ⅲ"},
	{TencentCode: "pt01801995", TencentName: "电视广播Ⅱ", EastMoneyBoardCode: "BK1219", EastMoneyBoardName: "电视广播Ⅱ"},
	{TencentCode: "pt01801219", TencentName: "酒店餐饮", EastMoneyBoardCode: "BK1271", EastMoneyBoardName: "酒店餐饮"},
	{TencentCode: "pt01801723", TencentName: "基础建设", EastMoneyBoardCode: "BK1247", EastMoneyBoardName: "基础建设"},
	{TencentCode: "pt01801126", TencentName: "非白酒", EastMoneyBoardCode: "BK1279", EastMoneyBoardName: "非白酒"},
	{TencentCode: "pt01801095", TencentName: "乘用车", EastMoneyBoardCode: "BK1262", EastMoneyBoardName: "乘用车"},
	{TencentCode: "pt01801991", TencentName: "航空机场", EastMoneyBoardCode: "BK0420", EastMoneyBoardName: "航空机场"},
	{TencentCode: "pt01801114", TencentName: "厨卫电器", EastMoneyBoardCode: "BK1240", EastMoneyBoardName: "厨卫电器"},
	{TencentCode: "pt01801113", TencentName: "小家电", EastMoneyBoardCode: "BK1244", EastMoneyBoardName: "小家电"},
	{TencentCode: "pt01801769", TencentName: "出版", EastMoneyBoardCode: "BK1218", EastMoneyBoardName: "出版"},
	{TencentCode: "pt01801125", TencentName: "白酒Ⅱ", EastMoneyBoardCode: "BK1277", EastMoneyBoardName: "白酒Ⅱ"},
	{TencentCode: "pt01801784", TencentName: "城商行Ⅱ", EastMoneyBoardCode: "BK1609", EastMoneyBoardName: "城商行Ⅲ"},
	{TencentCode: "pt01801179", TencentName: "铁路公路", EastMoneyBoardCode: "BK0421", EastMoneyBoardName: "铁路公路"},
	{TencentCode: "pt01801721", TencentName: "房屋建设Ⅱ", EastMoneyBoardCode: "BK1246", EastMoneyBoardName: "房屋建设Ⅱ"},
	{TencentCode: "pt01801783", TencentName: "股份制银行Ⅱ", EastMoneyBoardCode: "BK1610", EastMoneyBoardName: "股份制银行Ⅲ"},
	{TencentCode: "pt01801782", TencentName: "国有大型银行Ⅱ", EastMoneyBoardCode: "BK1611", EastMoneyBoardName: "国有大型银行Ⅲ"},
	{TencentCode: "pt01801738", TencentName: "电网设备", EastMoneyBoardCode: "BK0457", EastMoneyBoardName: "电网设备"},
	{TencentCode: "pt01801111", TencentName: "白色家电", EastMoneyBoardCode: "BK1239", EastMoneyBoardName: "白色家电"},
	{TencentCode: "pt01801129", TencentName: "调味发酵品Ⅱ", EastMoneyBoardCode: "BK1278", EastMoneyBoardName: "调味发酵品Ⅱ"},
	{TencentCode: "pt01801735", TencentName: "光伏设备", EastMoneyBoardCode: "BK1031", EastMoneyBoardName: "光伏设备"},
}

var radarIndustryMappingsByCode, radarIndustryMappingsByName = indexRadarIndustryMappings(radarIndustryMappings)

func indexRadarIndustryMappings(items []radarIndustryMapping) (map[string]radarIndustryMapping, map[string]radarIndustryMapping) {
	byCode := make(map[string]radarIndustryMapping, len(items))
	byName := make(map[string]radarIndustryMapping, len(items))
	for _, item := range items {
		byCode[strings.TrimSpace(item.TencentCode)] = item
		byName[normalizeMembership(item.TencentName)] = item
	}
	return byCode, byName
}

func lookupRadarIndustryMapping(code string, name string) (radarIndustryMapping, bool) {
	if mapping, ok := radarIndustryMappingsByCode[strings.TrimSpace(code)]; ok {
		return mapping, true
	}
	mapping, ok := radarIndustryMappingsByName[normalizeMembership(name)]
	return mapping, ok
}
