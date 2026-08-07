package sector

import "easy-stock/backend/internal/narrative"

var themeTabNames = []string{"半导体", "半导体材料", "人形机器人", "物理AI", "航天", "医疗/医药", "电力", "电网设备", "电池", "有色金属", "化工", "消费电子", "旅游", "影视", "消费", "光通信"}

type Theme struct {
	ID     string
	Name   string
	Tabs   []string
	Groups []Group
}

type Group struct {
	ID    string
	Name  string
	Nodes []Node
}

type Node struct {
	ID            string
	Name          string
	Description   string
	BoardCode     string
	BoardKeywords []string
	Narrative     string
}

func Themes() []Theme {
	return []Theme{
		semiconductorTheme(),
		semiconductorMaterialsTheme(),
		humanoidRobotTheme(),
		physicalAITheme(),
		aerospaceTheme(),
		healthcareTheme(),
		electricPowerTheme(),
		gridEquipmentTheme(),
		batteryTheme(),
		nonferrousMetalsTheme(),
		chemicalTheme(),
		consumerElectronicsTheme(),
		tourismTheme(),
		filmMediaTheme(),
		consumptionTheme(),
		opticalCommunicationTheme(),
	}
}

func FindTheme(id string) (Theme, bool) {
	for _, theme := range Themes() {
		if theme.ID == id {
			return theme, true
		}
	}
	if theme, ok := findRadarMappedTheme(id); ok {
		return theme, true
	}
	if name, ok := narrative.ThemeName(id); ok {
		return trendTheme(id, name), true
	}
	return Theme{}, false
}

func trendTheme(id string, name string) Theme {
	aliases := narrative.Aliases(name)
	return Theme{
		ID:   id,
		Name: name,
		Tabs: []string{name},
		Groups: []Group{{
			ID:   "trading_narrative",
			Name: "炒作主线",
			Nodes: []Node{{
				ID:            "narrative_core",
				Name:          name,
				Description:   "依据个股概念组合与近期涨停共振归类，行业仅作为辅助身份。",
				BoardKeywords: aliases,
				Narrative:     name,
			}},
		}},
	}
}

func semiconductorMaterialsTheme() Theme {
	return Theme{
		ID:   "semiconductor_materials",
		Name: "半导体材料",
		Tabs: themeTabNames,
		Groups: []Group{
			{
				ID:   "demand_entry",
				Name: "需求入口",
				Nodes: []Node{
					{ID: "ai_hpc_chip", Name: "AI/HPC芯片", BoardKeywords: []string{"半导体", "AI芯片", "算力"}},
					{ID: "storage_hbm", Name: "存储/HBM", BoardKeywords: []string{"存储芯片", "存储器", "HBM"}},
					{ID: "power_device", Name: "功率/射频/光电", BoardKeywords: []string{"分立器件", "第三代半导体", "氮化镓"}},
				},
			},
			{
				ID:   "materials_core",
				Name: "半导体材料",
				Nodes: []Node{
					{ID: "photoresist", Name: "光刻胶", BoardKeywords: []string{"光刻胶", "光刻机"}},
					{ID: "silicon_wafer", Name: "硅片/硅材料", BoardKeywords: []string{"硅能源", "半导体材料", "硅片"}},
					{ID: "electronic_gas", Name: "电子特气", BoardKeywords: []string{"电子化学品", "电子特气"}},
					{ID: "target_material", Name: "靶材", BoardKeywords: []string{"小金属", "稀有金属", "靶材"}},
					{ID: "cmp_material", Name: "CMP抛光液/垫", BoardKeywords: []string{"电子化学品", "CMP"}},
				},
			},
			{
				ID:   "manufacturing_packaging",
				Name: "制造与封测材料",
				Nodes: []Node{
					{ID: "advanced_packaging", Name: "先进封装材料", BoardKeywords: []string{"先进封装", "Chiplet", "封装"}},
					{ID: "pcb_ccl", Name: "PCB/覆铜板", BoardKeywords: []string{"PCB", "覆铜板"}},
					{ID: "substrate", Name: "封装基板", BoardKeywords: []string{"封装基板", "先进封装"}},
					{ID: "low_k", Name: "低介电材料", BoardKeywords: []string{"低介电", "电子化学品"}},
				},
			},
			{
				ID:   "equipment_related",
				Name: "设备相关材料",
				Nodes: []Node{
					{ID: "semiconductor_equipment", Name: "半导体设备", BoardKeywords: []string{"半导体设备", "光刻机"}},
					{ID: "cleanroom", Name: "洁净室材料", BoardKeywords: []string{"洁净室", "半导体"}},
					{ID: "vacuum", Name: "真空/气体设备", BoardKeywords: []string{"真空", "气体", "半导体设备"}},
				},
			},
		},
	}
}

func simpleTheme(id string, name string, groups ...Group) Theme {
	return Theme{ID: id, Name: name, Tabs: themeTabNames, Groups: groups}
}

func group(id string, name string, nodes ...Node) Group {
	return Group{ID: id, Name: name, Nodes: nodes}
}

func node(id string, name string, keywords []string) Node {
	return Node{ID: id, Name: name, BoardKeywords: keywords}
}

func semiconductorTheme() Theme {
	return simpleTheme("semiconductor", "半导体",
		group("core", "核心板块",
			node("semiconductor_core", "半导体", []string{"半导体"}),
			node("chip_design", "芯片设计", []string{"数字芯片设计", "模拟芯片设计", "AI芯片", "国产芯片"}),
			node("semiconductor_equipment", "半导体设备", []string{"半导体设备"}),
			node("advanced_packaging", "先进封装", []string{"先进封装"}),
			node("third_generation", "第三代半导体", []string{"第三代半导体"}),
		),
	)
}

func humanoidRobotTheme() Theme {
	return simpleTheme("humanoid_robot", "人形机器人",
		group("core", "机器人本体",
			node("humanoid_robot", "人形机器人", []string{"人形机器人"}),
			node("robot", "机器人", []string{"机器人", "机器人概念"}),
			node("robot_actuator", "机器人执行器", []string{"机器人执行器"}),
			node("servo", "伺服/运动控制", []string{"伺服系统", "电机", "自动化设备"}),
			node("machine_vision", "机器视觉/传感", []string{"机器视觉", "传感器"}),
		),
	)
}

func physicalAITheme() Theme {
	return simpleTheme("physical_ai", "物理AI",
		group("ai_entry", "AI入口",
			node("ai_agent", "AI智能体", []string{"AI智能体", "多模态AI", "AIGC概念"}),
			node("ai_chip", "AI芯片", []string{"AI芯片", "算力概念"}),
			node("edge_device", "AI终端", []string{"AI手机", "AIPC", "AI眼镜"}),
			node("robot_embodied", "具身智能/机器人", []string{"人形机器人", "机器人"}),
		),
	)
}

func aerospaceTheme() Theme {
	return simpleTheme("aerospace", "航天",
		group("core", "航天航空",
			node("aerospace", "航天航空", []string{"航天航空", "航天装备"}),
			node("commercial_space", "商业航天", []string{"商业航天"}),
			node("satellite", "卫星互联网", []string{"卫星互联网", "卫星导航"}),
			node("aviation", "航空装备", []string{"航空装备", "通用航空"}),
		),
	)
}

func healthcareTheme() Theme {
	return simpleTheme("healthcare", "医疗/医药",
		group("core", "医药医疗",
			node("medicine", "医药生物", []string{"医药生物", "生物医药", "化学制药", "创新药"}),
			node("medical_device", "医疗器械", []string{"医疗器械", "医疗设备", "医疗耗材"}),
			node("medical_service", "医疗服务", []string{"医疗服务", "创新医疗服务", "医疗研发外包", "CRO"}),
			node("traditional_medicine", "中药", []string{"中药", "中药概念"}),
			node("ai_medicine", "AI制药", []string{"AI制药（医疗）", "AI制药", "精准医疗"}),
		),
	)
}

func electricPowerTheme() Theme {
	return simpleTheme("electric_power", "电力",
		group("core", "电力运营",
			node("power", "电力", []string{"电力"}),
			node("green_power", "绿色电力", []string{"绿色电力"}),
			node("power_equipment", "电力设备", []string{"电力设备", "综合电力设备商"}),
			node("virtual_power", "虚拟电厂", []string{"虚拟电厂", "智能电网"}),
		),
	)
}

func gridEquipmentTheme() Theme {
	return simpleTheme("grid_equipment", "电网设备",
		group("core", "电网设备",
			node("grid_equipment", "电网设备", []string{"电网设备"}),
			node("grid_automation", "电网自动化", []string{"电网自动化设备", "智能电网"}),
			node("uhv", "特高压", []string{"特高压"}),
			node("power_transform", "输变电设备", []string{"输变电设备", "电气设备"}),
		),
	)
}

func batteryTheme() Theme {
	return simpleTheme("battery", "电池",
		group("core", "电池产业链",
			node("battery_core", "电池", []string{"电池"}),
			node("lithium_battery", "锂电池", []string{"锂电池", "锂电池概念"}),
			node("battery_chemical", "电池化学品", []string{"电池化学品"}),
			node("storage", "储能", []string{"储能概念", "熔盐储能"}),
			node("new_battery", "新型电池", []string{"固态电池", "钠离子电池", "钒电池"}),
		),
	)
}

func nonferrousMetalsTheme() Theme {
	return simpleTheme("nonferrous_metals", "有色金属",
		group("core", "金属资源",
			node("nonferrous", "有色金属", []string{"有色金属"}),
			node("small_metals", "小金属", []string{"小金属", "其他小金属"}),
			node("rare_earth", "稀土永磁", []string{"稀土永磁", "稀土"}),
			node("copper", "铜", []string{"铜", "工业金属"}),
			node("lithium", "锂/能源金属", []string{"锂", "能源金属", "锂矿概念"}),
		),
	)
}

func chemicalTheme() Theme {
	return simpleTheme("chemical", "化工",
		group("core", "化工产业",
			node("basic_chemical", "基础化工", []string{"基础化工", "化工原料"}),
			node("phosphorus", "磷化工", []string{"磷化工", "磷肥及磷化工"}),
			node("fluorine", "氟化工", []string{"氟化工", "氟化工概念"}),
			node("coal_chemical", "煤化工", []string{"煤化工", "煤化工概念"}),
			node("electronic_chemical", "电子化学品", []string{"电子化学品", "电子化学品Ⅲ"}),
		),
	)
}

func consumerElectronicsTheme() Theme {
	return simpleTheme("consumer_electronics", "消费电子",
		group("core", "消费电子",
			node("consumer_electronics", "消费电子", []string{"消费电子", "消费电子概念"}),
			node("brand_consumer", "品牌消费电子", []string{"品牌消费电子"}),
			node("components", "零部件及组装", []string{"消费电子零部件及组装"}),
			node("apple_chain", "苹果产业链", []string{"苹果概念"}),
			node("ai_device", "AI终端", []string{"AI手机", "AIPC", "AI眼镜"}),
		),
	)
}

func tourismTheme() Theme {
	return simpleTheme("tourism", "旅游",
		group("core", "旅游出行",
			node("tourism_hotel", "旅游酒店", []string{"旅游酒店", "旅游概念"}),
			node("scenic", "旅游及景区", []string{"旅游及景区", "旅游综合"}),
			node("travel_retail", "旅游零售", []string{"旅游零售"}),
			node("aviation_airport", "航空机场", []string{"航空机场", "航空运输"}),
		),
	)
}

func filmMediaTheme() Theme {
	return simpleTheme("film_media", "影视",
		group("core", "影视传媒",
			node("film_concept", "影视概念", []string{"影视概念"}),
			node("cinema", "影视院线", []string{"影视院线"}),
			node("animation", "影视动漫制作", []string{"影视动漫制作"}),
			node("cultural_media", "文娱消费", []string{"文娱消费", "文化传媒"}),
		),
	)
}

func consumptionTheme() Theme {
	return simpleTheme("consumption", "消费",
		group("core", "大消费",
			node("consumer_style", "消费风格", []string{"消费风格", "新消费"}),
			node("food_drink", "食品饮料", []string{"食品饮料", "白酒"}),
			node("retail", "零售", []string{"商业百货", "旅游零售"}),
			node("home_appliance", "家电", []string{"白色家电", "家电行业"}),
			node("consumer_electronics_entry", "消费电子", []string{"消费电子"}),
		),
	)
}

func opticalCommunicationTheme() Theme {
	return simpleTheme("optical_communication", "光通信",
		group("core", "光通信",
			node("optical_module", "光通信模块", []string{"光通信模块"}),
			node("cpo", "CPO", []string{"CPO概念"}),
			node("communication_equipment", "通信设备", []string{"通信设备", "通信服务"}),
			node("data_center", "数据中心/算力", []string{"算力概念", "数据中心"}),
		),
	)
}
