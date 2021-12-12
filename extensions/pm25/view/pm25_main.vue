<template>
    <div class="height-100">
        <div class="title">{{ $t("COMMON.TEXT3") }}</div>
        <div class="datas height-15">
            <div class="datas_bg">
                <div class="datas_top">
                    <img :src="weatherinfo.now.weather_pic" alt="" class="datas_top_icon">
                    <div class="tem_num">{{weatherinfo.now.temperature}}°</div>
                </div>
                <div class="datas_bottom">
                    <div class="times_up">
                        <img src="@/assets/images/sun_up.png" alt="" class="timesicon">
                        <span class="times_num">{{weatherinfo.sunset}}</span>
                        <img src="@/assets/images/sun_down.png" alt="" class="timesicon">
                        <span class="times_num">{{weatherinfo.sunrise}}</span>
                    </div>
                    <!--<div class="times_down">
                        <img src="@/assets/images/sun_down.png" alt="" class="timesicon">
                        <span class="times_num">{{weatherinfo.sunrise}}</span>
                    </div>-->
                </div>
            </div>
        </div>
        <div class="air_quality height-85">
            <div class="air_dash_box height-30">
                <div class="air_quality_title">{{ $t("COMMON.TEXT4") }}</div>
                <echarts
                        v-loading="loading"
                        id="chart"
                        ref="chart"
                        class="chart height-100 chart_dashboard"
                        @click="handleChartClick"
                        @init="chartInit"
                        :auto-resize="true"
                        :options="options">
                </echarts>
            </div>
            <div class="ul_box height-65">
                <ul class="numlist height-100">
                <li class="numitem">
                    <img src="@/assets/images/co2.png" alt="" class="numicon">
                    <div class="num_name inline-block pos-top-10">
                        <div class="num_title">{{ $t("COMMON.TEXT5") }}</div>
                        <div class="num_des">{{ $t("COMMON.TEXT10") }}</div>
                    </div>
                    <div class="num_data inline-block pull-right pos-top-20">
                        {{weatherinfo.now.aqiDetail.co2}}<span>mg/h</span>
                    </div>
                </li>
                <li class="numitem">
                    <img src="@/assets/images/o3.png" alt="" class="numicon">
                    <div class="num_name inline-block pos-top-10">
                        <div class="num_title">{{ $t("COMMON.TEXT6") }}</div>
                        <div class="num_des">{{ $t("COMMON.TEXT10") }}</div>
                    </div>
                    <div class="num_data inline-block pull-right pos-top-20">
                        {{weatherinfo.now.aqiDetail.o3}}<span>mg/h</span>
                    </div>
                </li>
                <li class="numitem">
                    <img src="@/assets/images/so2.png" alt="" class="numicon">
                    <div class="num_name inline-block pos-top-10">
                        <div class="num_title">{{ $t("COMMON.TEXT7") }}</div>
                        <div class="num_des">{{ $t("COMMON.TEXT10") }}</div>
                    </div>
                    <div class="num_data inline-block pull-right pos-top-20">
                        {{weatherinfo.now.aqiDetail.so2}}<span>mg/h</span>
                    </div>
                </li>
                <li class="numitem numitem_active">
                    <img src="@/assets/images/nai.png" alt="" class="numicon">
                    <div class="num_name inline-block pos-top-10">
                        <div class="num_title">{{ $t("COMMON.TEXT8") }}</div>
                        <div class="num_des">{{ $t("COMMON.TEXT10") }}</div>
                    </div>
                    <div class="num_data inline-block pull-right pos-top-20">
                        {{weatherinfo.now.aqiDetail.nai}}<span>mg/h</span>
                    </div>
                </li>
                <li class="numitem">
                    <img src="@/assets/images/co.png" alt="" class="numicon">
                    <div class="num_name inline-block pos-top-10">
                        <div class="num_title">{{ $t("COMMON.TEXT9") }}</div>
                        <div class="num_des">{{ $t("COMMON.TEXT10") }}</div>
                    </div>
                    <div class="num_data inline-block pull-right pos-top-20">
                        {{weatherinfo.now.aqiDetail.co}}<span>mg/h</span>
                    </div>
                </li>
            </ul>
            </div>
        </div>
    </div>
</template>
<style scoped src="@/assets/css/style.css"></style>
<script>
    import {LOGIN, LOGOUT} from "@/core/services/store/auth.module";
    import AUTH from "@/core/services/store/auth.module";
    import ApiService from "@/core/services/api.service";
    import websocket from "@/utils/websocket";
    export default {
        name: 'XAirQuality',
        props: {
            loading: {
                type: Boolean,
                default: true,
            },
            legend: {
                type: Boolean,
                default: true,
            },
            apiData: {
                type: Object
            },
            title: {
                type: String,
                default: '',
            },
            fields: {
                type: Object,
            },
            colorStart: {
                type: String,
                default: '#7956EC',
            },
            colorEnd: {
                type: String,
                default: '#3CECCF',
            },
        },
        data() {
            const self = this;
            return {
                chart: null,
                options: {},
                weatherinfo:{},
                center: [121.59996, 31.197646],
                lng: 0,
                lat: 0,
                loaded: false,
            };
        },
        computed: {},
        watch: {
            apiData: {
                immediate: true,
                handler(val, oldVal) {
                    var _this = this;
                    if (!_this.loading) {
                        _this.initChart();
                    }
                },
            },
            colorStart() {
                this.initChart();
            },
            colorEnd() {
                this.initChart();
            },
            legend(val, oldVal) {
                this.chart.setOption({
                    legend: {
                        show: val,
                    },
                });
            },
        },
        methods: {
            handleChartClick(param) {
                console.log(param);
            },

            /**
             * echarts instance init event
             * @param {object} chart echartsInstance
             */
            chartInit(chart) {
                this.chart = chart;
                // must resize chart in nextTick
                this.$nextTick(() => {
                    this.resizeChart();
                });
            },

            /**
             * emit chart component init event
             */
            emitInit() {
                if (this.$refs.chart) {
                    this.chart = this.$refs.chart.chart;
                    this.$emit('init', {
                        chart: this.chart,
                        chartData: this.apiData,
                    });
                }
            },

            /**
             * resize chart
             */
            resizeChart() {
                /* eslint-disable no-unused-expressions */
                this.chart && this.chart.resize();
            },

            /**
             * init chart
             */
            initChart() {
                console.log('空气质量');
                console.log(this.apiData);
                this.weatherinfo = this.apiData;
                this.gaugeimg('chart', 'PM2.5', 0, 500, this.weatherinfo.now.aqi, 'mg/m³');
            },

            /*
             *id:id;
             *title:仪表盘名称
             *min:最小值
             *max:最大值
             *val:当前实际值
             *unit:单位符号
             */
            gaugeimg(id, title, min, max, val, unit) {
                // var myChart = chart.init(document.getElementById(id)); //初始化

                this.options = {
                    title: {
                        text: title,
                        x: 'center',
                        y: '48%',
                        textStyle: { // 其余属性默认使用全局文本样式，详见TEXTSTYLE
                            color: '#4E4DB0',
                            fontWeight: 'bolder',
                            "fontSize": 13
                        },
                    },
                    tooltip: {
                        formatter: "{a} <br/>{b} : {c}" + unit
                    },
                    toolbox: {
                        show: false,
                        feature: {
                            mark: {
                                show: true
                            },
                            restore: {
                                show: true
                            },
                            saveAsImage: {
                                show: true
                            }
                        }
                    },
                    series: [{
                        center: ['50%', '55%'],
                        // number: [0, '100%'],
                        startAngle: 180, //仪表盘起始角度
                        endAngle: 0, //仪表盘结束角度
                        //min: min,
                        //max: max,
                        splitNumber: 10, //分割段数
                        name: title,
                        type: 'gauge',
                        radius: '90%',
                        axisLine: { // 坐标轴线
                            lineStyle: { // 属性lineStyle控制线条样式
                                color: [
                                    [0.25, '#ddd'],
                                    [1, '#ddd']
                                ],
                                width: 20
                            }
                        },
                        axisTick: { // 坐标轴小标记
                            show:false,
                            splitNumber: 10, // 每份split细分多少段
                            length: 12, // 属性length控制线长
                            lineStyle: { // 属性lineStyle控制线条样式
                                color: '#ddd'
                            }
                        },
                        axisLabel: { // 坐标轴文本标签，详见axis.axisLabel
                            show:false,
                            textStyle: { // 其余属性默认使用全局文本样式，详见TEXTSTYLE
                                color: '#aaa',
                                fontSize: 12,
                            },
                            "padding": [-5, -8],
                        },
                        splitLine: { // 分隔线
                            show: false, // 默认显示，属性show控制显示与否
                            length: 22, // 属性length控制线长
                            lineStyle: { // 属性lineStyle（详见lineStyle）控制线条样式
                                color: 'auto'
                            }
                        },
                        pointer: { //指针粗细
                            show:true,
                            width: 5
                        },
                        title: {
                            textStyle: { // 其余属性默认使用全局文本样式，详见TEXTSTYLE
                                fontWeight: 'bolder',
                                color: "#9b9b9b",
                                fontSize: 14,
                            },
                            "show": true,
                            "offsetCenter": [0, "-110%"],
                            "padding": [5, 10],
                            "fontSize": 14,
                        },
                        detail: {
                            formatter: '{value}' + unit,
                            textStyle: { // 其余属性默认使用全局文本样式，详见TEXTSTYLE
                                color: '#4E4DB0',
                                fontWeight: 'bolder',
                                "fontSize": 18
                            },
                            "offsetCenter": [0, "-30%"],
                        },

                        data: [{
                            //value: val,
                            //name: name
                        }]
                    }]
                };
                this.options.series[0].min = min;
                this.options.series[0].max = max;
                this.options.series[0].data[0].value = val;
                this.options.series[0].axisLine.lineStyle.color[0][0] = (val - min) / (max - min);
                this.options.series[0].axisLine.lineStyle.color[0][1] = this.detectionData(val, id);
                // myChart.setOption(option);
            },


            /*
             *颜色设置，
             */
            detectionData(str, id) {
                var color = {
                    // type: 'linear',
                    x: 0,
                    y: 0,
                    x2: 1,
                    y2: 0,
                    colorStops: [{
                        offset: 0, color: '#97ecf3', // 0%
                    }, {
                        offset: 1, color: '#4726a4', // 100%
                    }],
                };
                this.options.series[0].data[0].name = '优';
                if (str >= 101 && str <= 200) {
                    color={
                        // type: 'linear',
                        x: 0,
                            y: 0,
                            x2: 1,
                            y2: 0,
                            colorStops: [{
                            offset: 0, color: '#97ecf3', // 0%
                        }, {
                            offset: 1, color: '#4726a4', // 100%
                        }],
                    };
                        this.options.series[0].data[0].name = '良';
                }
                if (str >= 201 && str <= 300) {
                    color={
                        // type: 'linear',
                        x: 0,
                        y: 0,
                        x2: 1,
                        y2: 0,
                        colorStops: [{
                            offset: 0, color: '#97ecf3', // 0%
                        }, {
                            offset: 1, color: '#4726a4', // 100%
                        }],
                    };
                        this.options.series[0].data[0].name = '轻度污染';
                }
                if (str >= 301 && str <= 400) {
                    color={
                        // type: 'linear',
                        x: 0,
                        y: 0,
                        x2: 1,
                        y2: 0,
                        colorStops: [{
                            offset: 0, color: '#97ecf3', // 0%
                        }, {
                            offset: 1, color: '#4726a4', // 100%
                        }],
                    };
                        this.options.series[0].data[0].name = '中度污染';
                }
                if (str >= 401 && str <= 500) {
                    color={
                        // type: 'linear',
                        x: 0,
                        y: 0,
                        x2: 1,
                        y2: 0,
                        colorStops: [{
                            offset: 0, color: '#97ecf3', // 0%
                        }, {
                            offset: 1, color: '#4726a4', // 100%
                        }],
                    };
                        this.options.series[0].data[0].name = '重度污染';
                }
                this.options.series[0].axisLine.lineStyle.width = '18'; //重置仪表盘轴线宽度
                this.options.series[0].axisTick.length = '16'; //重置仪表盘刻度线长度
                this.options.series[0].title.color = color.colorStops[1].color; //字体颜色和轴线颜色一致
                this.options.series[0].title.fontSize = 30; //第一个字体变大


                return color;
            },


        },
        created(){
            var _this = this;
            navigator.geolocation.getCurrentPosition(function(data){
                console.log(data)
                var logt = [data.coords.longitude,data.coords.latitude];
                console.log(logt);
                //Push message data to the server and store it in kv
                _this.$emit('send', {
                    logt: logt
                });
            });
        },
        async mounted() {
            var _this = this;
            // this.emitInit();
            /*setInterval(function(){
                _this.airquality();
            },5000);*/
        }
    };
</script>
