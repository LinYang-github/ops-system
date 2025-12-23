import http from 'k6/http';
import { check, sleep } from 'k6';

// 配置：模拟 50 个并发用户
export const options = {
  vus: 50,
  duration: '30s',
};

const BASE_URL = 'http://localhost:8080';

export default function () {
  // 1. 获取系统列表 (读性能)
  const resList = http.get(`${BASE_URL}/api/systems`);
  check(resList, {
    'status is 200': (r) => r.status === 200,
    'duration < 200ms': (r) => r.timings.duration < 200,
  });

  // 2. 模拟创建系统 (写性能)
  const payload = JSON.stringify({
    name: `PerfTest-${__VU}-${__ITER}`,
    description: 'Load testing system creation'
  });
  
  const params = {
    headers: { 'Content-Type': 'application/json' },
  };

  const resCreate = http.post(`${BASE_URL}/api/systems/create`, payload, params);
  check(resCreate, {
    'create status 200': (r) => r.status === 200,
  });

  sleep(1);
}