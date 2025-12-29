package scheduler

import (
	"log"

	"ops-system/pkg/protocol"
)

type Scheduler struct{}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// SelectBestNode 从节点列表中选择负载最低的一个
// 返回: 节点IP, 是否找到
func (s *Scheduler) SelectBestNode(nodes []protocol.NodeInfo) (string, bool) {
	var bestNode string
	var maxScore float64 = -1.0

	candidates := 0

	for _, node := range nodes {
		// 1. 过滤离线节点
		if node.Status != "online" {
			continue
		}

		// 2. 计算闲置资源得分 (权重: CPU 60%, 内存 40%)
		// CPU/Mem 越低，(100-Usage) 越大，得分越高
		cpuScore := 100 - node.CPUUsage
		memScore := 100 - node.MemUsage

		// 简单的加权算法
		score := (cpuScore * 0.6) + (memScore * 0.4)

		// 3. 择优 (打擂台)
		if score > maxScore {
			maxScore = score
			bestNode = node.IP
		}
		candidates++
	}

	if candidates == 0 {
		log.Println("[Scheduler] No online nodes available")
		return "", false
	}

	log.Printf("[Scheduler] Selected %s (Score: %.2f)", bestNode, maxScore)
	return bestNode, true
}
