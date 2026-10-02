import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  App as AntdApp,
  Button,
  Calendar,
  Checkbox,
  DatePicker,
  Dropdown,
  Modal,
  Radio,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { LeftOutlined, RightOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'

import {
  CalendarService,
  DATASET_SHIFT,
  DATASET_TEACHER,
  DUTY_OFF,
  DUTY_REQUIRED,
  DUTY_UNSPECIFIED,
  MetadataService,
  WEEKDAY_LABELS,
  errorText,
  tablePagination,
} from '../api'
import type { DatasetView, DutyView } from '../api'

type Props = { scopeId: number }

const DUTY_OPTIONS = [
  { label: '未指定', value: DUTY_UNSPECIFIED },
  { label: '值班', value: DUTY_REQUIRED },
  { label: '不值班', value: DUTY_OFF },
]

/** 日历页：勾选星期生成排班日历，点击日期可反选排班日或配置老师值班。 */
export default function CalendarPage({ scopeId }: Props) {
  const { message } = AntdApp.useApp()

  const [startDate, setStartDate] = useState<Dayjs | null>(null)
  const [endDate, setEndDate] = useState<Dayjs | null>(null)
  const [weekdays, setWeekdays] = useState<boolean[]>(Array(7).fill(false))
  const [solver, setSolver] = useState('')
  const [cursor, setCursor] = useState<Dayjs>(dayjs())
  const [generating, setGenerating] = useState(false)

  const [days, setDays] = useState<string[]>([])
  const [duties, setDuties] = useState<DutyView[]>([])
  const [teachers, setTeachers] = useState<DatasetView[]>([])
  const [shifts, setShifts] = useState<DatasetView[]>([])

  const [activeDay, setActiveDay] = useState<string | null>(null)
  const [dutyDay, setDutyDay] = useState<string | null>(null)
  const [dutyShiftID, setDutyShiftID] = useState<number | undefined>(undefined)
  const [dutyDraft, setDutyDraft] = useState<Record<number, string>>({})
  const [savingDuty, setSavingDuty] = useState(false)

  const load = useCallback(async () => {
    try {
      const [calendar, dayList, dutyList, teacherList, shiftList] = await Promise.all([
        CalendarService.GetCalendar(scopeId),
        CalendarService.ListScheduleDays(scopeId),
        CalendarService.ListDuties(scopeId),
        MetadataService.ListDatasets(scopeId, DATASET_TEACHER, ''),
        MetadataService.ListDatasets(scopeId, DATASET_SHIFT, ''),
      ])
      if (calendar) {
        setStartDate(dayjs(calendar.startDate))
        setEndDate(dayjs(calendar.endDate))
        const flags = calendar.weekdays
        if (flags && flags.length === 7) setWeekdays(flags.map(Boolean))
        setSolver(calendar.solver)
        setCursor(dayjs(calendar.startDate))
      } else {
        setStartDate(null)
        setEndDate(null)
      }
      setDays(dayList ?? [])
      setDuties(dutyList ?? [])
      setTeachers(teacherList ?? [])
      setShifts(shiftList ?? [])
    } catch (err) {
      message.error(errorText(err))
    }
  }, [scopeId, message])

  useEffect(() => {
    void load()
  }, [load])

  const daySet = useMemo(() => new Set(days), [days])
  const dutyByDay = useMemo(() => {
    const grouped = new Map<string, DutyView[]>()
    for (const duty of duties) {
      const list = grouped.get(duty.day)
      if (list) list.push(duty)
      else grouped.set(duty.day, [duty])
    }
    return grouped
  }, [duties])

  // 只展示配置过值班或不值班的老师；一个都没有时整张表不显示
  const dutyTeachers = useMemo(
    () => teachers.filter((teacher) => duties.some((duty) => duty.teacherId === teacher.id)),
    [teachers, duties],
  )

  const generate = async () => {
    if (!startDate || !endDate) {
      message.warning('请选择开始和结束日期')
      return
    }
    if (!weekdays.some(Boolean)) {
      message.warning('请至少选择一个星期')
      return
    }
    setGenerating(true)
    try {
      await CalendarService.SaveCalendar(scopeId, {
        startDate: startDate.format('YYYY-MM-DD'),
        endDate: endDate.format('YYYY-MM-DD'),
        weekdays,
        solver,
      })
      message.success('已生成排班日历')
      await load()
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setGenerating(false)
    }
  }

  const buildDraft = (day: string, shiftID: number | undefined) => {
    const draft: Record<number, string> = {}
    for (const teacher of teachers) {
      const duty = duties.find((d) => d.day === day && d.shiftId === shiftID && d.teacherId === teacher.id)
      draft[teacher.id] = duty ? (duty.required ? DUTY_REQUIRED : DUTY_OFF) : DUTY_UNSPECIFIED
    }
    return draft
  }

  const openDutyModal = (day: string) => {
    const shiftID = shifts[0]?.id
    setDutyDay(day)
    setDutyShiftID(shiftID)
    setDutyDraft(buildDraft(day, shiftID))
  }

  const changeDutyShift = (shiftID: number) => {
    setDutyShiftID(shiftID)
    if (dutyDay) setDutyDraft(buildDraft(dutyDay, shiftID))
  }

  const saveDuty = async () => {
    if (!dutyDay || !dutyShiftID) {
      message.warning('请先选择班次')
      return
    }
    setSavingDuty(true)
    try {
      for (const teacher of teachers) {
        await CalendarService.SetDuty(
          scopeId,
          dutyDay,
          dutyShiftID,
          teacher.id,
          dutyDraft[teacher.id] ?? DUTY_UNSPECIFIED,
        )
      }
      setDutyDay(null)
      message.success('保存成功')
      await load()
    } catch (err) {
      message.error(errorText(err))
    } finally {
      setSavingDuty(false)
    }
  }

  const onMenuClick = async (key: string, day: string) => {
    if (key === 'toggle') {
      try {
        const scheduled = await CalendarService.ToggleScheduleDay(scopeId, day)
        message.success(scheduled ? `${day} 已加入排班日` : `${day} 已排除排班日`)
        await load()
      } catch (err) {
        message.error(errorText(err))
      }
      return
    }
    openDutyModal(day)
  }

  const renderCell = (date: Dayjs) => {
    if (date.year() !== cursor.year() || date.month() !== cursor.month()) return null
    const day = date.format('YYYY-MM-DD')
    const scheduled = daySet.has(day)
    const cellDuties = dutyByDay.get(day) ?? []

    // 颜色：排班浅蓝、不排班浅红、有人值班浅绿、有人不值班浅橙、两者都有浅紫
    const hasDutyOn = cellDuties.some((d) => d.required)
    const hasDutyOff = cellDuties.some((d) => !d.required)
    let cellClass = 'cal-cell'
    if (hasDutyOn && hasDutyOff) cellClass += ' cal-cell-both'
    else if (hasDutyOn) cellClass += ' cal-cell-duty-on'
    else if (hasDutyOff) cellClass += ' cal-cell-duty-off'
    else if (scheduled) cellClass += ' cal-cell-on'
    else cellClass += ' cal-cell-off'
    if (activeDay === day) cellClass += ' cal-cell-active'

    return (
      <Dropdown
        trigger={['click']}
        menu={{
          items: [
            { key: 'toggle', label: '排班日选择/反选' },
            { key: 'duty', label: '老师班次值班/不值班' },
          ],
          onClick: ({ key }) => void onMenuClick(key, day),
        }}
        onOpenChange={(open) => {
          if (open) setActiveDay(day)
        }}
      >
        <div className={cellClass}>
          <div className="cal-day">{date.format('DD')}</div>
          {cellDuties.length > 0 ? (
            cellDuties.map((duty, index) => (
              <div key={index} className="cal-duty">
                {`${duty.teacher} ${duty.shift} ${duty.required ? '值班' : '不值班'}`}
              </div>
            ))
          ) : (
            <div className="cal-state">{scheduled ? '排班' : '不排班'}</div>
          )}
        </div>
      </Dropdown>
    )
  }

  const renderDutyTags = (teacherID: number, required: boolean) => {
    const list = duties.filter((d) => d.teacherId === teacherID && d.required === required)
    if (list.length === 0) return '无'
    return (
      <Space size={4} wrap>
        {list.map((duty, index) => (
          <Tag key={index} color={required ? 'blue' : 'red'}>
            {`${duty.day.slice(5)} ${duty.shift}`}
          </Tag>
        ))}
      </Space>
    )
  }

  const dutyColumns: TableColumnsType<DatasetView> = [
    { title: '老师', dataIndex: 'name', width: 200 },
    { title: '值班', render: (_, row) => renderDutyTags(row.id, true) },
    { title: '不值班', render: (_, row) => renderDutyTags(row.id, false) },
  ]

  return (
    <>
      <Space wrap style={{ marginBottom: 16 }}>
        <DatePicker value={startDate} onChange={setStartDate} placeholder="开始日期" />
        <DatePicker value={endDate} onChange={setEndDate} placeholder="结束日期" />
        {WEEKDAY_LABELS.map((label, index) => (
          <Checkbox
            key={label}
            checked={weekdays[index]}
            onChange={(e) =>
              setWeekdays(weekdays.map((value, i) => (i === index ? e.target.checked : value)))
            }
          >
            {label}
          </Checkbox>
        ))}
        <Button type="primary" loading={generating} onClick={() => void generate()}>
          生成
        </Button>
      </Space>

      <Calendar
        fullscreen
        value={cursor}
        onChange={(date) => setCursor(date)}
        headerRender={({ value, onChange }) => (
          <div className="cal-header">
            <Button icon={<LeftOutlined />} onClick={() => onChange(value.clone().subtract(1, 'month'))} />
            <Typography.Text strong>{value.format('YYYY年M月')}</Typography.Text>
            <Button icon={<RightOutlined />} onClick={() => onChange(value.clone().add(1, 'month'))} />
          </div>
        )}
        fullCellRender={(date) => renderCell(date)}
      />

      {dutyTeachers.length > 0 && (
        <Table
          style={{ marginTop: 24 }}
          rowKey="id"
          columns={dutyColumns}
          dataSource={dutyTeachers}
          pagination={tablePagination()}
        />
      )}

      <Modal
        open={dutyDay !== null}
        title={dutyDay ? `${dutyDay} 老师班次值班/不值班` : ''}
        okText="确定"
        cancelText="取消"
        confirmLoading={savingDuty}
        onCancel={() => setDutyDay(null)}
        onOk={() => void saveDuty()}
        destroyOnHidden
      >
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Select
            style={{ width: '100%' }}
            placeholder="班次"
            value={dutyShiftID}
            options={shifts.map((shift) => ({ label: shift.name, value: shift.id }))}
            onChange={changeDutyShift}
          />
          {teachers.length === 0 ? (
            <Typography.Text type="secondary">还没有老师，请先在老师页添加</Typography.Text>
          ) : (
            teachers.map((teacher) => (
              <div key={teacher.id} style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <span style={{ width: 90 }}>{teacher.name}</span>
                <Radio.Group
                  optionType="button"
                  value={dutyDraft[teacher.id] ?? DUTY_UNSPECIFIED}
                  options={DUTY_OPTIONS}
                  onChange={(e) =>
                    setDutyDraft({ ...dutyDraft, [teacher.id]: e.target.value as string })
                  }
                />
              </div>
            ))
          )}
        </Space>
      </Modal>
    </>
  )
}
